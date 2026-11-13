package panel

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

type subscriptionSyncStatus struct {
	ApplyState     ConfigApplyState `json:"apply_state"`
	PendingServers int              `json:"pending_servers"`
	FailedServers  int              `json:"failed_servers"`
}

// Only managed nodes with a granted multi-user inbound (directly or via an
// enabled inbound-targeting PortForward) derive credentials from this account.
// Raw, shared-credential and external nodes must stay untouched.
func subscriptionSyncServerIDs(db *gorm.DB, user *model.User) ([]uint, error) {
	if len(user.ServerIDs) == 0 && len(user.ForwardIDs) == 0 {
		return nil, nil
	}
	var ids []uint
	if len(user.ServerIDs) > 0 {
		var servers []model.Server
		if err := db.Select("id", "config_mode", "raw_config").Where("id IN ?", user.ServerIDs).Find(&servers).Error; err != nil {
			return nil, err
		}
		var managedIDs []uint
		for _, server := range servers {
			if server.EffectiveConfigMode() == model.ConfigModeManaged {
				managedIDs = append(managedIDs, server.ID)
			}
		}
		if len(managedIDs) > 0 {
			var inbounds []model.Inbound
			if err := db.Select("id", "server_id", "type", "settings").Where("server_id IN ?", managedIDs).Find(&inbounds).Error; err != nil {
				return nil, err
			}
			for _, inbound := range inbounds {
				if !user.HasInbound(inbound.ServerID, inbound.ID) {
					continue
				}
				multiUser, err := inboundUsesMultiUser(inbound)
				if err != nil {
					return nil, err
				}
				if multiUser {
					ids = append(ids, inbound.ServerID)
				}
			}
		}
	}
	if len(user.ForwardIDs) > 0 {
		var forwards []model.PortForward
		if err := db.Select("target_inbound_id").
			Where("id IN ? AND target_type = ? AND target_inbound_id > 0 AND enabled = ?", user.ForwardIDs, model.ForwardTargetInbound, true).
			Find(&forwards).Error; err != nil {
			return nil, err
		}
		targetInboundIDs := make([]uint, 0, len(forwards))
		for _, f := range forwards {
			if f.TargetInboundID > 0 {
				targetInboundIDs = append(targetInboundIDs, f.TargetInboundID)
			}
		}
		targetInboundIDs = normalizedIDs(targetInboundIDs)
		if len(targetInboundIDs) > 0 {
			var inbounds []model.Inbound
			if err := db.Select("id", "server_id", "type", "settings").Where("id IN ?", targetInboundIDs).Find(&inbounds).Error; err != nil {
				return nil, err
			}
			targetServerIDs := make([]uint, 0, len(inbounds))
			for _, inbound := range inbounds {
				targetServerIDs = append(targetServerIDs, inbound.ServerID)
			}
			targetServerIDs = normalizedIDs(targetServerIDs)
			if len(targetServerIDs) > 0 {
				var servers []model.Server
				if err := db.Select("id", "config_mode", "raw_config").Where("id IN ?", targetServerIDs).Find(&servers).Error; err != nil {
					return nil, err
				}
				managed := make(map[uint]bool, len(servers))
				for _, server := range servers {
					if server.EffectiveConfigMode() == model.ConfigModeManaged {
						managed[server.ID] = true
					}
				}
				for _, inbound := range inbounds {
					if !managed[inbound.ServerID] {
						continue
					}
					multiUser, err := inboundUsesMultiUser(inbound)
					if err != nil {
						return nil, err
					}
					if multiUser {
						ids = append(ids, inbound.ServerID)
					}
				}
			}
		}
	}
	return normalizedIDs(ids), nil
}

func inboundUsesMultiUser(inbound model.Inbound) (bool, error) {
	if len(inbound.Settings) == 0 {
		return false, nil
	}
	var settings singbox.InboundSettings
	if err := json.Unmarshal(inbound.Settings, &settings); err != nil {
		return false, fmt.Errorf("read inbound %d credential settings: %w", inbound.ID, err)
	}
	return settings.UseMultiUser(string(inbound.Type)), nil
}

func readSubscriptionSyncStatus(db *gorm.DB, serverIDs []uint) (subscriptionSyncStatus, error) {
	status := subscriptionSyncStatus{ApplyState: ConfigApplied}
	if len(serverIDs) == 0 {
		return status, nil
	}
	var pending []model.Server
	if err := db.Select("proxy_sync_failed").Where("id IN ? AND proxy_sync_revision > proxy_sync_applied_revision", serverIDs).Find(&pending).Error; err != nil {
		return status, err
	}
	status.PendingServers = len(pending)
	for _, server := range pending {
		if server.ProxySyncFailed {
			status.FailedServers++
		}
	}
	if status.FailedServers > 0 {
		status.ApplyState = ConfigFailed
	} else if status.PendingServers > 0 {
		status.ApplyState = ConfigPending
	}
	return status, nil
}

func (a *App) handleSubscriptionSync(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	var user model.User
	if err := a.db.First(&user, currentUID(c)).Error; err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "暂时无法读取同步状态"})
		return
	}
	ids, err := subscriptionSyncServerIDs(a.db, &user)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "暂时无法读取同步状态"})
		return
	}
	status, err := readSubscriptionSyncStatus(a.db, ids)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "暂时无法读取同步状态"})
		return
	}
	c.JSON(http.StatusOK, status)
}
