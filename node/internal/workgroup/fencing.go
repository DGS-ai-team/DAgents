package workgroup

// CheckCommandFencing 校验 ToolCommand 相对 Binding / Tombstone / catalog 的栅栏。
func CheckCommandFencing(cmd ToolCommand, binding WorkerBinding, catalogRev string, tombstone *ArchiveTombstone) error {
	if tombstone != nil && tombstone.WorkgroupID == cmd.WorkgroupID {
		if cmd.LeaseEpoch < tombstone.LeaseEpochAtArchive {
			return errf(CodeFencingRejected, "command lease_epoch %d < archive epoch %d", cmd.LeaseEpoch, tombstone.LeaseEpochAtArchive)
		}
		return errf(CodeWorkgroupArchived, "workgroup archived")
	}
	if binding.Status == "archived" {
		return errf(CodeWorkgroupArchived, "member binding archived")
	}
	if cmd.LeaseEpoch != binding.LeaseEpoch {
		return errf(CodeFencingRejected, "lease_epoch mismatch cmd=%d binding=%d", cmd.LeaseEpoch, binding.LeaseEpoch)
	}
	if cmd.MemberGeneration != binding.MemberGeneration {
		return errf(CodeFencingRejected, "member_generation mismatch")
	}
	if cmd.MemberSpecDigest != binding.MemberSpecDigest {
		return errf(CodeDigestMismatch, "member_spec_digest mismatch")
	}
	if catalogRev != "" && cmd.ToolCatalogRevision != "" && cmd.ToolCatalogRevision != catalogRev {
		return errf(CodeCatalogDrift, "tool_catalog_revision cmd=%s node=%s", cmd.ToolCatalogRevision, catalogRev)
	}
	return nil
}

// EffectiveToolNames = SpecAllow ∩ NodeAvailable（第二参数保留兼容，与 SpecAllow 相同即可）。
// SpecAllow 为空 ⇒ 无工具（fail-closed）。
func EffectiveToolNames(specAllow, grantAllow, nodeAvailable []string) []string {
	if len(specAllow) == 0 {
		return nil
	}
	grantSet := toSet(grantAllow)
	nodeSet := toSet(nodeAvailable)
	out := make([]string, 0, len(specAllow))
	seen := map[string]struct{}{}
	for _, name := range specAllow {
		if name == "" {
			continue
		}
		if _, ok := grantSet[name]; !ok {
			continue
		}
		if len(nodeSet) > 0 {
			if _, ok := nodeSet[name]; !ok {
				continue
			}
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func toSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		out[item] = struct{}{}
	}
	return out
}

func mergeToolNames(parts ...[]string) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, part := range parts {
		for _, name := range part {
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}
