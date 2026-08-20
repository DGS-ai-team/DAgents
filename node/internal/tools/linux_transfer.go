package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
)

const (
	// DefaultLinuxTransferConcurrency is deliberately conservative for a Node
	// that may also be serving interactive terminals and LLM requests.
	DefaultLinuxTransferConcurrency = 2
	DefaultLinuxTransferQueueLimit  = 32
	DefaultLinuxTransferTimeout     = 10 * time.Minute
	MaxLinuxTransferBytes           = 100 << 20
	maxLinuxTransferPathBytes       = 4096
)

type LinuxTransferEventSink func(agentID, eventType string, data map[string]any, replayable bool)

type LinuxTransferRequest struct {
	AgentID    string
	ToolCallID string
	ChannelID  string
	Direction  string
	LocalPath  string
	RemotePath string
	Overwrite  bool
}

type LinuxTransferSnapshot struct {
	TransferID string    `json:"transfer_id"`
	AgentID    string    `json:"agent_id"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	ChannelID  string    `json:"channel_id"`
	Direction  string    `json:"direction"`
	LocalPath  string    `json:"local_path"`
	RemotePath string    `json:"remote_path"`
	Status     string    `json:"status"`
	BytesDone  int64     `json:"bytes_done"`
	TotalBytes int64     `json:"total_bytes"`
	Progress   int       `json:"progress"`
	SpeedBPS   int64     `json:"speed_bps,omitempty"`
	Error      string    `json:"error,omitempty"`
	Result     string    `json:"result,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type linuxTransferJob struct {
	mu         sync.Mutex
	id         string
	request    LinuxTransferRequest
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	status     string
	bytesDone  int64
	totalBytes int64
	startedAt  time.Time
	createdAt  time.Time
	updatedAt  time.Time
	lastEmitAt time.Time
	speedBPS   int64
	err        error
	result     string
	finished   bool
}

// LinuxTransferManager owns the process-wide queue. A transfer is counted as
// one file regardless of its direction; queued work waits FIFO for a slot.
type LinuxTransferManager struct {
	provider *LinuxShellProvider
	fsRoot   string
	max      int
	queueMax int
	sink     LinuxTransferEventSink

	mu      sync.Mutex
	active  int
	pending []*linuxTransferJob
	jobs    map[string]*linuxTransferJob
}

var linuxTransferSequence uint64

func NewLinuxTransferManager(provider *LinuxShellProvider, fsRoot string, maxConcurrent int, sink LinuxTransferEventSink) *LinuxTransferManager {
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultLinuxTransferConcurrency
	}
	if maxConcurrent > 8 {
		maxConcurrent = 8
	}
	root, err := filepath.Abs(strings.TrimSpace(fsRoot))
	if err != nil || root == "" {
		root = "."
	}
	return &LinuxTransferManager{
		provider: provider,
		fsRoot:   root,
		max:      maxConcurrent,
		queueMax: DefaultLinuxTransferQueueLimit,
		sink:     sink,
		jobs:     make(map[string]*linuxTransferJob),
	}
}

func (m *LinuxTransferManager) MaxConcurrent() int {
	if m == nil || m.max <= 0 {
		return DefaultLinuxTransferConcurrency
	}
	return m.max
}

func (m *LinuxTransferManager) Submit(ctx context.Context, req LinuxTransferRequest) (string, error) {
	if m == nil || m.provider == nil {
		return "", fmt.Errorf("linux transfer is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateLinuxTransferRequest(req); err != nil {
		return "", err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	now := time.Now().UTC()
	job := &linuxTransferJob{
		id:        fmt.Sprintf("transfer-%d", atomic.AddUint64(&linuxTransferSequence, 1)),
		request:   req,
		ctx:       jobCtx,
		cancel:    cancel,
		done:      make(chan struct{}),
		status:    "queued",
		createdAt: now,
		updatedAt: now,
	}
	m.mu.Lock()
	if len(m.pending) >= m.queueMax {
		m.mu.Unlock()
		cancel()
		return "", fmt.Errorf("linux transfer queue is full (max %d)", m.queueMax)
	}
	m.jobs[job.id] = job
	m.pending = append(m.pending, job)
	m.mu.Unlock()
	m.emit(job, true)
	m.schedule()

	select {
	case <-job.done:
		job.mu.Lock()
		defer job.mu.Unlock()
		if job.err != nil {
			return job.result, job.err
		}
		return job.result, nil
	case <-ctx.Done():
		_ = m.Cancel(job.id)
		return "", ctx.Err()
	}
}

func (m *LinuxTransferManager) List(agentID string) []LinuxTransferSnapshot {
	if m == nil {
		return []LinuxTransferSnapshot{}
	}
	filter := strings.TrimSpace(agentID)
	m.mu.Lock()
	jobs := make([]*linuxTransferJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		if filter != "" && strings.TrimSpace(job.request.AgentID) != filter {
			continue
		}
		jobs = append(jobs, job)
	}
	m.mu.Unlock()
	items := make([]LinuxTransferSnapshot, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, job.snapshot())
	}
	// Stable newest-first order makes the status bar deterministic after reload.
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.After(items[i].CreatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items
}

func (m *LinuxTransferManager) Cancel(transferID string) bool {
	if m == nil {
		return false
	}
	id := strings.TrimSpace(transferID)
	if id == "" {
		return false
	}
	var queued *linuxTransferJob
	m.mu.Lock()
	job := m.jobs[id]
	if job == nil {
		m.mu.Unlock()
		return false
	}
	job.mu.Lock()
	if job.finished || job.status == "completed" || job.status == "failed" || job.status == "cancelled" {
		job.mu.Unlock()
		m.mu.Unlock()
		return false
	}
	if job.status == "queued" {
		for i, pending := range m.pending {
			if pending == job {
				m.pending = append(m.pending[:i], m.pending[i+1:]...)
				break
			}
		}
		job.status = "cancelled"
		job.err = context.Canceled
		job.updatedAt = time.Now().UTC()
		job.finished = true
		queued = job
		close(job.done)
		job.mu.Unlock()
		m.mu.Unlock()
		job.cancel()
		m.emit(queued, true)
		m.schedule()
		return true
	}
	cancel := job.cancel
	job.mu.Unlock()
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (m *LinuxTransferManager) schedule() {
	if m == nil {
		return
	}
	var start []*linuxTransferJob
	m.mu.Lock()
	for m.active < m.max && len(m.pending) > 0 {
		job := m.pending[0]
		m.pending = m.pending[1:]
		job.mu.Lock()
		if job.finished || job.ctx.Err() != nil {
			job.status = "cancelled"
			job.err = context.Canceled
			job.updatedAt = time.Now().UTC()
			job.finished = true
			close(job.done)
			job.mu.Unlock()
			continue
		}
		job.status = "transferring"
		job.startedAt = time.Now().UTC()
		job.updatedAt = job.startedAt
		job.mu.Unlock()
		m.active++
		start = append(start, job)
	}
	m.mu.Unlock()
	for _, job := range start {
		m.emit(job, true)
		go m.run(job)
	}
}

func (m *LinuxTransferManager) run(job *linuxTransferJob) {
	result, err := m.execute(job)
	m.mu.Lock()
	m.active--
	m.mu.Unlock()
	job.mu.Lock()
	if job.ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		job.status = "cancelled"
		if err == nil {
			err = job.ctx.Err()
		}
	} else if err != nil {
		job.status = "failed"
	} else {
		job.status = "completed"
	}
	job.result = result
	job.err = err
	job.updatedAt = time.Now().UTC()
	job.finished = true
	close(job.done)
	job.mu.Unlock()
	job.cancel()
	m.emit(job, true)
	m.schedule()
}

func (m *LinuxTransferManager) execute(job *linuxTransferJob) (string, error) {
	ctx, cancel := context.WithTimeout(job.ctx, DefaultLinuxTransferTimeout)
	defer cancel()
	_, client, agentConn, err := m.provider.openClient(ctx, job.request.ChannelID, job.request.AgentID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	if agentConn != nil {
		defer agentConn.Close()
	}
	closeOnCancel := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-closeOnCancel:
		}
	}()
	defer close(closeOnCancel)

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("sftp session failed: %w", err)
	}
	defer sftpClient.Close()
	if job.request.Direction == "upload" {
		return m.upload(ctx, sftpClient, job)
	}
	return m.download(ctx, sftpClient, job)
}

func (m *LinuxTransferManager) upload(ctx context.Context, client *sftp.Client, job *linuxTransferJob) (string, error) {
	local, err := strictTransferPath(m.fsRoot, job.request.LocalPath, true)
	if err != nil {
		return "", err
	}
	source, err := os.Open(local)
	if err != nil {
		return "", fmt.Errorf("open local file: %w", err)
	}
	defer source.Close()
	stat, err := source.Stat()
	if err != nil {
		return "", err
	}
	if !stat.Mode().IsRegular() {
		return "", fmt.Errorf("local path is not a regular file")
	}
	if stat.Size() > MaxLinuxTransferBytes {
		return "", fmt.Errorf("file exceeds maximum transfer size of %d bytes", MaxLinuxTransferBytes)
	}
	m.setTotal(job, stat.Size())
	remote := cleanRemoteTransferPath(job.request.RemotePath)
	if err := ensureRemotePath(remote); err != nil {
		return "", err
	}
	if !job.request.Overwrite {
		if _, err := client.Stat(remote); err == nil {
			return "", fmt.Errorf("remote file already exists: %s", remote)
		} else if !isRemoteNotExist(err) {
			return "", fmt.Errorf("check remote file: %w", err)
		}
	}
	temp := remote + ".dagents-part-" + job.id
	defer client.Remove(temp)
	target, err := client.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return "", fmt.Errorf("open remote file: %w", err)
	}
	hash := sha256.New()
	writer := io.MultiWriter(target, hash)
	_, copyErr := io.Copy(writer, &transferProgressReader{reader: source, manager: m, job: job})
	closeErr := target.Close()
	if copyErr != nil {
		return "", fmt.Errorf("upload file: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close remote file: %w", closeErr)
	}
	if job.ctx.Err() != nil {
		return "", job.ctx.Err()
	}
	if job.request.Overwrite {
		if err := client.Remove(remote); err != nil && !isRemoteNotExist(err) {
			return "", fmt.Errorf("replace remote file: %w", err)
		}
	}
	if err := client.Rename(temp, remote); err != nil {
		return "", fmt.Errorf("commit remote file: %w", err)
	}
	return transferResult(job, stat.Size(), hex.EncodeToString(hash.Sum(nil))), nil
}

func (m *LinuxTransferManager) download(ctx context.Context, client *sftp.Client, job *linuxTransferJob) (string, error) {
	local, err := strictTransferPath(m.fsRoot, job.request.LocalPath, false)
	if err != nil {
		return "", err
	}
	remote := cleanRemoteTransferPath(job.request.RemotePath)
	if err := ensureRemotePath(remote); err != nil {
		return "", err
	}
	source, err := client.Open(remote)
	if err != nil {
		return "", fmt.Errorf("open remote file: %w", err)
	}
	defer source.Close()
	stat, err := source.Stat()
	if err != nil {
		return "", err
	}
	if !stat.Mode().IsRegular() {
		return "", fmt.Errorf("remote path is not a regular file")
	}
	if stat.Size() > MaxLinuxTransferBytes {
		return "", fmt.Errorf("file exceeds maximum transfer size of %d bytes", MaxLinuxTransferBytes)
	}
	m.setTotal(job, stat.Size())
	if !job.request.Overwrite {
		if _, err := os.Stat(local); err == nil {
			return "", fmt.Errorf("local file already exists: %s", job.request.LocalPath)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("check local file: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return "", fmt.Errorf("create local directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(local), ".dagents-transfer-*")
	if err != nil {
		return "", fmt.Errorf("create local temporary file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temp, hash), &transferProgressReader{reader: source, manager: m, job: job})
	closeErr := temp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("download file: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close local file: %w", closeErr)
	}
	if job.ctx.Err() != nil {
		return "", job.ctx.Err()
	}
	if job.request.Overwrite {
		if err := os.Remove(local); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("replace local file: %w", err)
		}
	}
	if err := os.Rename(tempName, local); err != nil {
		return "", fmt.Errorf("commit local file: %w", err)
	}
	return transferResult(job, stat.Size(), hex.EncodeToString(hash.Sum(nil))), nil
}

type transferProgressReader struct {
	reader  io.Reader
	manager *LinuxTransferManager
	job     *linuxTransferJob
}

func (r *transferProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.manager.addProgress(r.job, int64(n))
	}
	return n, err
}

func (m *LinuxTransferManager) setTotal(job *linuxTransferJob, total int64) {
	job.mu.Lock()
	job.totalBytes = total
	job.updatedAt = time.Now().UTC()
	job.mu.Unlock()
	m.emit(job, false)
}

func (m *LinuxTransferManager) addProgress(job *linuxTransferJob, n int64) {
	job.mu.Lock()
	now := time.Now().UTC()
	job.bytesDone += n
	if !job.startedAt.IsZero() {
		seconds := time.Since(job.startedAt).Seconds()
		if seconds > 0 {
			job.speedBPS = int64(float64(job.bytesDone) / seconds)
		}
	}
	job.updatedAt = now
	shouldEmit := job.lastEmitAt.IsZero() || now.Sub(job.lastEmitAt) >= 100*time.Millisecond ||
		(job.totalBytes > 0 && job.bytesDone >= job.totalBytes)
	if shouldEmit {
		job.lastEmitAt = now
	}
	job.mu.Unlock()
	if shouldEmit {
		m.emit(job, false)
	}
}

func (m *LinuxTransferManager) emit(job *linuxTransferJob, replayable bool) {
	if m == nil || m.sink == nil || job == nil {
		return
	}
	snapshot := job.snapshot()
	data := map[string]any{
		"transfer_id":  snapshot.TransferID,
		"agent_id":     snapshot.AgentID,
		"tool_call_id": snapshot.ToolCallID,
		"channel_id":   snapshot.ChannelID,
		"direction":    snapshot.Direction,
		"local_path":   snapshot.LocalPath,
		"remote_path":  snapshot.RemotePath,
		"status":       snapshot.Status,
		"bytes_done":   snapshot.BytesDone,
		"total_bytes":  snapshot.TotalBytes,
		"progress":     snapshot.Progress,
		"speed_bps":    snapshot.SpeedBPS,
		"error":        snapshot.Error,
		"result":       snapshot.Result,
		"created_at":   snapshot.CreatedAt,
		"updated_at":   snapshot.UpdatedAt,
	}
	m.sink(snapshot.AgentID, "transfer.updated", data, replayable)
}

func (j *linuxTransferJob) snapshot() LinuxTransferSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	progress := 0
	if j.totalBytes > 0 {
		progress = int(float64(j.bytesDone) / float64(j.totalBytes) * 100)
		if progress > 100 {
			progress = 100
		}
	}
	errText := ""
	if j.err != nil {
		errText = j.err.Error()
	}
	return LinuxTransferSnapshot{
		TransferID: j.id,
		AgentID:    j.request.AgentID,
		ToolCallID: j.request.ToolCallID,
		ChannelID:  j.request.ChannelID,
		Direction:  j.request.Direction,
		LocalPath:  j.request.LocalPath,
		RemotePath: j.request.RemotePath,
		Status:     j.status,
		BytesDone:  j.bytesDone,
		TotalBytes: j.totalBytes,
		Progress:   progress,
		SpeedBPS:   j.speedBPS,
		Error:      errText,
		Result:     j.result,
		CreatedAt:  j.createdAt,
		UpdatedAt:  j.updatedAt,
	}
}

func transferResult(job *linuxTransferJob, bytes int64, sha string) string {
	data, _ := json.Marshal(map[string]any{
		"transfer_id": job.id,
		"status":      "completed",
		"direction":   job.request.Direction,
		"bytes":       bytes,
		"sha256":      sha,
		"local_path":  job.request.LocalPath,
		"remote_path": job.request.RemotePath,
	})
	return string(data)
}

func validateLinuxTransferRequest(req LinuxTransferRequest) error {
	if strings.TrimSpace(req.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	if strings.TrimSpace(req.ChannelID) == "" {
		return fmt.Errorf("channel_id is required")
	}
	if req.Direction != "upload" && req.Direction != "download" {
		return fmt.Errorf("direction must be upload or download")
	}
	if strings.TrimSpace(req.LocalPath) == "" || strings.TrimSpace(req.RemotePath) == "" {
		return fmt.Errorf("local_path and remote_path are required")
	}
	if len(req.LocalPath) > maxLinuxTransferPathBytes || len(req.RemotePath) > maxLinuxTransferPathBytes {
		return fmt.Errorf("transfer path is too long")
	}
	return nil
}

func strictTransferPath(root, raw string, mustExist bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("local_path is required")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("local_path must be relative to the Node workspace")
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("local_path escapes the Node workspace")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil {
		return "", err
	}
	if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("local_path escapes the Node workspace")
	}
	if mustExist {
		if _, err := os.Stat(full); err != nil {
			return "", err
		}
	}
	return full, nil
}

func cleanRemoteTransferPath(raw string) string {
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if value == "" {
		return ""
	}
	return path.Clean(value)
}

func ensureRemotePath(value string) error {
	if value == "" || value == "." || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("remote_path is invalid")
	}
	return nil
}

func isRemoteNotExist(err error) bool {
	return err != nil && (errors.Is(err, os.ErrNotExist) || strings.Contains(strings.ToLower(err.Error()), "no such file"))
}

type linuxFileTransferArgs struct {
	ChannelID  string `json:"channel_id"`
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Overwrite  bool   `json:"overwrite"`
}

func linuxFileTransferToolDefs() []ToolDef {
	base := map[string]any{
		"channel_id":  map[string]any{"type": "string", "description": "已绑定到当前 Agent 的 Linux channel ID。"},
		"local_path":  map[string]any{"type": "string", "description": "Node 工作区内的相对文件路径。"},
		"remote_path": map[string]any{"type": "string", "description": "远程 Linux 主机上的文件路径。"},
		"overwrite":   map[string]any{"type": "boolean", "description": "目标文件存在时是否覆盖，默认 false。"},
	}
	return []ToolDef{
		{Type: "function", Function: FunctionDef{
			Name:        "linux_file_upload",
			Description: "通过指定的 Linux SSH 配置将 Node 工作区中的单个文件上传到远程主机。任务可能排队；返回表示传输完成，文件内容不会写入消息历史。",
			Parameters:  injectCallPurposeParam(objectParams(base, "channel_id", "local_path", "remote_path")),
		}},
		{Type: "function", Function: FunctionDef{
			Name:        "linux_file_download",
			Description: "通过指定的 Linux SSH 配置将远程主机中的单个文件下载到 Node 工作区。任务可能排队；返回表示传输完成，文件内容不会写入消息历史。",
			Parameters:  injectCallPurposeParam(objectParams(base, "channel_id", "local_path", "remote_path")),
		}},
	}
}

func (r *Registry) execLinuxFileUpload(ctx context.Context, raw json.RawMessage) (string, error) {
	return r.execLinuxFileTransfer(ctx, raw, "upload")
}

func (r *Registry) execLinuxFileDownload(ctx context.Context, raw json.RawMessage) (string, error) {
	return r.execLinuxFileTransfer(ctx, raw, "download")
}

func (r *Registry) execLinuxFileTransfer(ctx context.Context, raw json.RawMessage, direction string) (string, error) {
	if r == nil || r.linuxTransferManager == nil {
		return "", fmt.Errorf("linux file transfer is not configured")
	}
	var args linuxFileTransferArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	result, err := r.linuxTransferManager.Submit(ctx, LinuxTransferRequest{
		AgentID:    r.agentID,
		ToolCallID: toolCallIDFromContext(ctx),
		ChannelID:  strings.TrimSpace(args.ChannelID),
		Direction:  direction,
		LocalPath:  strings.TrimSpace(args.LocalPath),
		RemotePath: strings.TrimSpace(args.RemotePath),
		Overwrite:  args.Overwrite,
	})
	if err != nil {
		return result, err
	}
	return result, nil
}
