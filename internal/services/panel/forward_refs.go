package panel

import (
	"context"
	"fmt"
	"strings"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
	"gorm.io/gorm"
)

// resolveForwardTarget computes the effective host:port for a PortForward.
func resolveForwardTarget(
	db *gorm.DB,
	relayServerID uint,
	fwd *model.PortForward,
	requireEnabledTarget bool,
) (addr string, port int, targetSrv *model.Server, targetIb *model.Inbound, err error) {
	targetType := strings.TrimSpace(fwd.TargetType)
	if targetType == "" || targetType == model.ForwardTargetCustom {
		cleanAddr, err := singbox.ValidateForwardTargetSyntax(fwd.TargetAddress)
		if err != nil {
			return "", 0, nil, nil, fmt.Errorf("目标地址无效: %w", err)
		}
		if singbox.IsDisallowedForwardTarget(cleanAddr) {
			return "", 0, nil, nil, fmt.Errorf("转发目标不能指向本机回环、未指定或链路本地地址")
		}
		if fwd.TargetPort < 1 || fwd.TargetPort > 65535 {
			return "", 0, nil, nil, fmt.Errorf("目标端口必须在 1-65535 之间")
		}
		return cleanAddr, fwd.TargetPort, nil, nil, nil
	}

	if targetType != model.ForwardTargetInbound {
		return "", 0, nil, nil, fmt.Errorf("不支持的转发目标类型 %q", targetType)
	}
	if fwd.TargetInboundID == 0 {
		return "", 0, nil, nil, fmt.Errorf("请选择目标入站")
	}

	var ib model.Inbound
	if err := db.First(&ib, fwd.TargetInboundID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", 0, nil, nil, fmt.Errorf("目标入站不存在")
		}
		return "", 0, nil, nil, err
	}
	if ib.ServerID == relayServerID {
		return "", 0, nil, nil, fmt.Errorf("端口转发的目标入站不能位于当前服务器自身")
	}
	if requireEnabledTarget && !ib.Enabled {
		return "", 0, nil, nil, fmt.Errorf("目标入站 %q 已停用，请先启用或更换目标", ib.Tag)
	}
	netProto := strings.ToLower(strings.TrimSpace(fwd.Network))
	if netProto == "tcp" && (ib.Type == "hysteria2" || ib.Type == "hysteria" || ib.Type == "tuic") {
		return "", 0, nil, nil, fmt.Errorf("目标入站 %q（%s）基于 QUIC/UDP，不能仅转发 TCP；请选择 UDP 或 TCP/UDP", ib.Tag, ib.Type)
	}

	var srv model.Server
	if err := db.First(&srv, ib.ServerID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", 0, nil, nil, fmt.Errorf("目标入站所属服务器不存在")
		}
		return "", 0, nil, nil, err
	}
	cleanAddr, err := singbox.ValidateForwardTargetSyntax(srv.Address)
	if err != nil {
		return "", 0, nil, nil, fmt.Errorf("目标服务器 %q 未配置有效的公网连接地址: %w", srv.Name, err)
	}
	if singbox.IsDisallowedForwardTarget(cleanAddr) {
		return "", 0, nil, nil, fmt.Errorf("目标服务器 %q 的连接地址 %q 不能用于端口转发", srv.Name, cleanAddr)
	}
	if ib.ListenPort < 1 || ib.ListenPort > 65535 {
		return "", 0, nil, nil, fmt.Errorf("目标入站端口 %d 无效", ib.ListenPort)
	}
	return cleanAddr, ib.ListenPort, &srv, &ib, nil
}

// hydratePortForwards batch-fills UI display metadata for PortForward slices.
func hydratePortForwards(db *gorm.DB, items []model.PortForward) error {
	if len(items) == 0 {
		return nil
	}
	inboundIDs := make([]uint, 0, len(items))
	inboundSeen := make(map[uint]bool)
	for _, f := range items {
		if f.TargetType == model.ForwardTargetInbound && f.TargetInboundID > 0 && !inboundSeen[f.TargetInboundID] {
			inboundSeen[f.TargetInboundID] = true
			inboundIDs = append(inboundIDs, f.TargetInboundID)
		}
	}

	inboundsByID := make(map[uint]model.Inbound, len(inboundIDs))
	serversByID := make(map[uint]model.Server)
	if len(inboundIDs) > 0 {
		var ibs []model.Inbound
		if err := db.Where("id IN ?", inboundIDs).Find(&ibs).Error; err != nil {
			return err
		}
		serverIDs := make([]uint, 0, len(ibs))
		serverSeen := make(map[uint]bool)
		for _, ib := range ibs {
			inboundsByID[ib.ID] = ib
			if ib.ServerID > 0 && !serverSeen[ib.ServerID] {
				serverSeen[ib.ServerID] = true
				serverIDs = append(serverIDs, ib.ServerID)
			}
		}
		if len(serverIDs) > 0 {
			var srvs []model.Server
			if err := db.Select("id", "name", "address").Where("id IN ?", serverIDs).Find(&srvs).Error; err != nil {
				return err
			}
			for _, s := range srvs {
				serversByID[s.ID] = s
			}
		}
	}

	for i := range items {
		f := &items[i]
		if f.TargetType == model.ForwardTargetInbound {
			if ib, ok := inboundsByID[f.TargetInboundID]; ok {
				f.TargetInboundTag = ib.Tag
				f.ResolvedPort = ib.ListenPort
				if s, ok := serversByID[ib.ServerID]; ok {
					f.TargetServerName = s.Name
					f.ResolvedAddress = s.Address
				}
			}
		} else {
			f.ResolvedAddress = f.TargetAddress
			f.ResolvedPort = f.TargetPort
		}
	}
	return nil
}

func buildServerForwardInputs(db *gorm.DB, serverID uint, nodeTags map[string]bool) ([]singbox.ForwardInput, error) {
	var forwards []model.PortForward
	if err := db.Where("server_id = ? AND enabled = ?", serverID, true).Order("id").Find(&forwards).Error; err != nil {
		return nil, err
	}
	out := make([]singbox.ForwardInput, 0, len(forwards))
	for _, f := range forwards {
		addr, port, _, _, err := resolveForwardTarget(db, serverID, &f, true)
		if err != nil {
			return nil, fmt.Errorf("port forward %q: %w", f.Tag, err)
		}
		outbound := strings.TrimSpace(f.Outbound)
		if outbound == "" {
			outbound = "direct"
		}
		if outbound != "direct" && nodeTags != nil {
			nodeTags[outbound] = true
		}
		out = append(out, singbox.ForwardInput{
			Tag:             f.Tag,
			ListenPort:      f.ListenPort,
			Network:         f.Network,
			OverrideAddress: addr,
			OverridePort:    port,
			Outbound:        outbound,
		})
	}
	return out, nil
}

func inboundForwardReferences(db *gorm.DB, inboundID uint) (total int64, enabled int64, tcpOnly int64, err error) {
	scope := db.Model(&model.PortForward{}).
		Where("target_type = ? AND target_inbound_id = ?", model.ForwardTargetInbound, inboundID)
	if err = scope.Count(&total).Error; err != nil {
		return 0, 0, 0, err
	}
	if err = db.Model(&model.PortForward{}).
		Where("target_type = ? AND target_inbound_id = ? AND enabled = ?", model.ForwardTargetInbound, inboundID, true).
		Count(&enabled).Error; err != nil {
		return 0, 0, 0, err
	}
	if err = db.Model(&model.PortForward{}).
		Where("target_type = ? AND target_inbound_id = ? AND network = ?", model.ForwardTargetInbound, inboundID, "tcp").
		Count(&tcpOnly).Error; err != nil {
		return 0, 0, 0, err
	}
	return total, enabled, tcpOnly, nil
}

func serverInboundForwardReferences(db *gorm.DB, targetServerID uint) (int64, error) {
	var count int64
	err := db.Model(&model.PortForward{}).
		Where("target_type = ? AND target_server_id = ? AND server_id <> ?", model.ForwardTargetInbound, targetServerID, targetServerID).
		Count(&count).Error
	return count, err
}

func outboundForwardReferences(db *gorm.DB, outboundTag string) (int64, error) {
	var count int64
	err := db.Model(&model.PortForward{}).Where("outbound = ?", outboundTag).Count(&count).Error
	return count, err
}

func rejectInboundForwardReferences(db *gorm.DB, inboundIDs []uint) error {
	if len(inboundIDs) == 0 {
		return nil
	}
	var count int64
	if err := db.Model(&model.PortForward{}).
		Where("target_type = ? AND target_inbound_id IN ?", model.ForwardTargetInbound, inboundIDs).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: 有 %d 条端口转发仍引用被移除的入站，请先修改或删除对应端口转发", errManagedInboundForwardProtected, count)
	}
	return nil
}

func renameForwardOutboundRefs(tx *gorm.DB, oldTag, nextTag string) (int64, error) {
	res := tx.Model(&model.PortForward{}).
		Where("outbound = ?", oldTag).
		Update("outbound", nextTag)
	return res.RowsAffected, res.Error
}

func distinctServerIDsFromForwards(db *gorm.DB, where string, args ...any) ([]uint, error) {
	var serverIDs []uint
	err := db.Model(&model.PortForward{}).
		Distinct("server_id").
		Where(where, args...).
		Pluck("server_id", &serverIDs).Error
	return serverIDs, err
}

func (a *App) pushManagedServers(serverIDs []uint) {
	if a.orch == nil {
		return
	}
	for _, sid := range serverIDs {
		a.orch.PushConfigIfManaged(sid)
	}
}

func (a *App) pushForwardsReferencingInbound(inboundID uint) {
	ids, err := distinctServerIDsFromForwards(
		a.db,
		"target_type = ? AND target_inbound_id = ? AND enabled = ?",
		model.ForwardTargetInbound, inboundID, true,
	)
	if err == nil {
		a.pushManagedServers(ids)
	}
}

func (a *App) pushForwardsReferencingServer(targetServerID uint) {
	ids, err := distinctServerIDsFromForwards(
		a.db,
		"target_type = ? AND target_server_id = ? AND enabled = ?",
		model.ForwardTargetInbound, targetServerID, true,
	)
	if err == nil {
		a.pushManagedServers(ids)
	}
}

func (a *App) pushForwardsUsingOutbound(outboundTag string) {
	ids, err := distinctServerIDsFromForwards(
		a.db,
		"outbound = ? AND enabled = ?",
		outboundTag, true,
	)
	if err == nil {
		a.pushManagedServers(ids)
	}
}

func (a *App) validateForwardOutbound(db *gorm.DB, serverID uint, outbound string) (string, error) {
	ob := strings.TrimSpace(outbound)
	if ob == "" {
		ob = "direct"
	}
	if ob == "block" || ob == "reject" || ob == "sniff" || ob == "hijack-dns" {
		return "", fmt.Errorf("端口转发的出站不能为 %q，请选择 direct 或节点出站", ob)
	}
	if err := validateManagedRoutingSource(db, serverID, ob); err != nil {
		return "", err
	}
	exists, err := a.outboundExists(db, ob)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("目标出站 %q 不存在", ob)
	}
	return ob, nil
}

func (a *App) validateForwardTagAndPort(db *gorm.DB, serverID, forwardID uint, tag string, listenPort int) error {
	if err := validateTag(tag); err != nil {
		return err
	}
	if listenPort < 1 || listenPort > 65535 {
		return fmt.Errorf("监听端口必须在 1-65535 之间")
	}
	if listenPort == 29091 || listenPort == 29092 {
		return fmt.Errorf("端口 %d 为系统流量统计保留端口", listenPort)
	}
	if err := checkForwardTrafficTag(db, serverID, forwardID, tag); err != nil {
		return err
	}

	var inbounds []model.Inbound
	if err := db.Where("server_id = ?", serverID).Find(&inbounds).Error; err != nil {
		return err
	}
	for _, ib := range inbounds {
		if ib.Tag == tag {
			return fmt.Errorf("标签 %q 与现有入站冲突", tag)
		}
		if ib.ListenPort == listenPort {
			return fmt.Errorf("监听端口 %d 与现有入站 %q 冲突", listenPort, ib.Tag)
		}
		if gTag, gPort, ok := inboundEffectiveGuard(ib); ok {
			if gTag == tag {
				return fmt.Errorf("标签 %q 与 %q 的 Reality 前置守卫标签冲突", tag, ib.Tag)
			}
			if gPort == listenPort {
				return fmt.Errorf("监听端口 %d 与 %q 的 Reality 前置守卫内部端口冲突", listenPort, ib.Tag)
			}
		}
	}

	var others []model.PortForward
	q := db.Where("server_id = ?", serverID)
	if forwardID > 0 {
		q = q.Where("id <> ?", forwardID)
	}
	if err := q.Find(&others).Error; err != nil {
		return err
	}
	for _, other := range others {
		if other.Tag == tag {
			return fmt.Errorf("标签 %q 已被同服务器其他端口转发使用", tag)
		}
		if other.ListenPort == listenPort {
			return fmt.Errorf("监听端口 %d 已被端口转发 %q 占用", listenPort, other.Tag)
		}
	}
	return nil
}

func deleteServerForwardState(tx *gorm.DB, serverID uint) error {
	for _, m := range []any{
		&model.PortForward{},
		&model.TrafficForwardRecord{},
		&model.TrafficForwardHourly{},
		&model.ForwardTrafficIdentity{},
	} {
		if err := tx.Where("server_id = ?", serverID).Delete(m).Error; err != nil {
			return err
		}
	}
	return nil
}

// collectInboundForwardOutboundConflicts rejects server address updates that would
// invalidate an enabled cross-server PortForward.
func checkServerAddressForForwards(db *gorm.DB, targetServerID uint, nextAddress string) error {
	var count int64
	if err := db.Model(&model.PortForward{}).
		Where("target_type = ? AND target_server_id = ? AND enabled = ?", model.ForwardTargetInbound, targetServerID, true).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	cleanAddr, err := singbox.ValidateForwardTargetSyntax(nextAddress)
	if err != nil {
		return fmt.Errorf("该服务器正被已启用的端口转发作为目标节点，新地址格式无效: %w", err)
	}
	if singbox.IsDisallowedForwardTarget(cleanAddr) {
		return fmt.Errorf("该服务器正被已启用的端口转发作为目标节点，连接地址不能改为回环、未指定或链路本地地址")
	}
	return nil
}

// Unused helper guard check ensures context import remains active if extended.
var _ context.Context
