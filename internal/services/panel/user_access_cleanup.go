package panel

import (
	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"gorm.io/gorm"
)

// removeServerUserAssignments runs after the server's inbounds are deleted in
// the same transaction. An empty explicit inbound list must never become a
// whole-server grant on one of the user's remaining servers.
func removeServerUserAssignments(tx *gorm.DB, serverID uint, deletedInboundIDs []uint) error {
	var users []model.User
	if err := tx.Find(&users).Error; err != nil {
		return err
	}
	deleted := make(map[uint]bool, len(deletedInboundIDs))
	for _, id := range deletedInboundIDs {
		deleted[id] = true
	}
	for i := range users {
		user := &users[i]
		affected := containsID(user.ServerIDs, serverID)
		for _, id := range user.InboundIDs {
			affected = affected || deleted[id]
		}
		if !affected {
			continue
		}
		servers := removeID(user.ServerIDs, serverID)
		if len(user.InboundIDs) > 0 {
			var remaining []model.Inbound
			if len(servers) > 0 {
				if err := tx.Select("id", "server_id").Where("id IN ? AND server_id IN ?", user.InboundIDs, servers).Find(&remaining).Error; err != nil {
					return err
				}
			}
			user.InboundIDs = make([]uint, 0, len(remaining))
			servers = make([]uint, 0, len(remaining))
			for _, inbound := range remaining {
				user.InboundIDs = append(user.InboundIDs, inbound.ID)
				servers = append(servers, inbound.ServerID)
			}
			user.InboundIDs = normalizedIDs(user.InboundIDs)
		}
		user.ServerIDs = normalizedIDs(servers)
		if err := tx.Model(user).Select("ServerIDs", "InboundIDs", "UpdatedAt").Updates(user).Error; err != nil {
			return err
		}
	}
	return nil
}
