package api

import (
	"net/http"
)

// tryEdgeUpgrade D5 Cut3：远程 Placement Edge 代理已停用。
// Manage edge 路由与 EdgeClient 仍保留，供后续整包拆除；产品流量不再升级。
func (s *Server) tryEdgeUpgrade(w http.ResponseWriter, r *http.Request) bool {
	_ = s
	_ = w
	_ = r
	return false
}
