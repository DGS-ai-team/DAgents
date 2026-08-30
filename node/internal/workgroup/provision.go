package workgroup

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Provisioner 处理幂等 member.provision。
type Provisioner struct {
	NodeID   string
	Bindings BindingStore
	// MemberTombstones prevents a provision replay from recreating a member
	// when the archive frame arrived before the binding existed locally.
	MemberTombstones map[string]ArchiveTombstone
	// NodeToolNames 为本机可提供的工具名（来自 tools.Registry）。
	NodeToolNames []string
	// WorkspaceCreated 测试钩子：统计新建 workspace 次数。
	WorkspaceCreated int
}

// Provision 实现 retry_same_id_ok / same_id_different_digest_conflict。
func (p *Provisioner) Provision(req ProvisionRequest) (*ProvisionResult, error) {
	if req.ProvisionID == "" || req.MemberID == "" || req.WorkgroupID == "" {
		return nil, errf(CodeSchemaMismatch, "provision_id/member_id/workgroup_id required")
	}
	if req.MemberSpecDigest == "" || req.LeaseEpoch < 1 || req.MemberGeneration < 1 {
		return nil, errf(CodeSchemaMismatch, "digest/lease_epoch/member_generation required")
	}
	if req.HomeNodeID != "" && p.NodeID != "" && req.HomeNodeID != p.NodeID {
		return nil, errf(CodeNotAuthorized, "home_node_id must be this node")
	}
	if tombstone, ok := p.MemberTombstones[memberTombstoneKey(req.WorkgroupID, req.MemberID)]; ok {
		return nil, errf(
			CodeWorkgroupArchived,
			"member archived at lease epoch %d",
			tombstone.LeaseEpochAtArchive,
		)
	}

	existing, err := p.Bindings.GetByProvisionID(req.ProvisionID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.MemberSpecDigest != req.MemberSpecDigest {
			return nil, errf(CodePayloadConflict, "same provision_id with different member_spec_digest")
		}
		// 幂等重试：不重建 workspace；补默认 README 供连通性探测
		_ = ensureDefaultREADME(existing.WorkspacePath)
		manifest := BuildManifest(p.NodeID, EffectiveToolNames(req.ToolAllowNames, p.NodeToolNames), WorkspaceToolSchemas(), WorkspaceSideEffectClasses())
		existing.ToolCatalogRevision = manifest.ToolCatalogRevision
		return &ProvisionResult{Binding: *existing, Created: false, Manifest: manifest}, nil
	}

	// 同 member 新 provision_id：member_generation 前进时替换绑定（PATCH 重配）；
	// 同代且 digest 变化视为冲突；更旧 generation 拒绝。
	byMember, err := p.Bindings.Get(req.MemberID)
	if err != nil {
		return nil, err
	}
	if byMember != nil && byMember.ProvisionID != "" && byMember.ProvisionID != req.ProvisionID {
		if req.MemberGeneration < byMember.MemberGeneration {
			return nil, errf(CodePayloadConflict, "stale member_generation")
		}
		if req.MemberGeneration == byMember.MemberGeneration &&
			byMember.MemberSpecDigest != req.MemberSpecDigest {
			return nil, errf(CodePayloadConflict, "member already provisioned with different digest")
		}
	}

	wsRoot := req.WorkspaceRoot
	if wsRoot == "" {
		wsRoot = filepath.Join(".", ".runtime", "workgroup-workers")
	}
	wsPath := filepath.Join(wsRoot, req.WorkgroupID, req.MemberID)
	created := false
	if _, err := os.Stat(wsPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Join(wsPath, "data"), 0o755); err != nil {
			return nil, err
		}
		created = true
		p.WorkspaceCreated++
	} else if err != nil {
		return nil, err
	}
	if err := ensureDefaultREADME(wsPath); err != nil {
		return nil, err
	}

	effective := EffectiveToolNames(req.ToolAllowNames, p.NodeToolNames)
	manifest := BuildManifest(p.NodeID, effective, WorkspaceToolSchemas(), WorkspaceSideEffectClasses())
	now := time.Now().UTC()
	binding := WorkerBinding{
		MemberID:                  req.MemberID,
		WorkgroupID:               req.WorkgroupID,
		HomeNodeID:                firstNonEmpty(req.HomeNodeID, p.NodeID),
		ProvisionID:               req.ProvisionID,
		MemberSpecDigest:          req.MemberSpecDigest,
		LeaseEpoch:                req.LeaseEpoch,
		MemberGeneration:          req.MemberGeneration,
		WorkspacePath:             wsPath,
		Status:                    "ready",
		NotEnumerableAsLocalAgent: true,
		ToolAllowNames:            append([]string(nil), effective...),
		ToolCatalogRevision:       manifest.ToolCatalogRevision,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
	if byMember != nil && !byMember.CreatedAt.IsZero() {
		binding.CreatedAt = byMember.CreatedAt
	}
	if err := p.Bindings.Put(binding); err != nil {
		return nil, err
	}
	return &ProvisionResult{Binding: binding, Created: created, Manifest: manifest}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func ensureDefaultREADME(wsPath string) error {
	if strings.TrimSpace(wsPath) == "" {
		return nil
	}
	readme := filepath.Join(wsPath, "README")
	if _, err := os.Stat(readme); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	content := "# Member workspace\n\nread_file paths are relative to this directory.\n"
	return os.WriteFile(readme, []byte(content), 0o644)
}
