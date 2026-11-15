package panel

import (
	"github.com/tokinx/singbox-panel/internal/domain/model"
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
		affected := containsID(user.ServerIDs, serverID) ||
			containsAnyID(user.InboundIDs, deleted) ||
			containsAnyID(user.InboundIPv6IDs, deleted)
		if !affected {
			continue
		}
		servers := removeID(user.ServerIDs, serverID)
		hadExplicit := len(user.InboundIDs) > 0 || len(user.InboundIPv6IDs) > 0
		if hadExplicit {
			if err := syncRemainingExplicitInboundAccess(tx, user, servers, deleted, true); err != nil {
				return err
			}
		} else {
			user.ServerIDs = normalizedIDs(servers)
		}
		if err := tx.Model(user).Select("ServerIDs", "InboundIDs", "InboundIPv6IDs", "UpdatedAt").Updates(user).Error; err != nil {
			return err
		}
	}
	return nil
}

// removeForwardUserAssignments removes deleted or no-longer-inbound PortForward
// references from every user's ForwardIDs list and mixed node order.
func removeForwardUserAssignments(tx *gorm.DB, forwardIDs []uint) error {
	forwardIDs = normalizedIDs(forwardIDs)
	if len(forwardIDs) == 0 {
		return nil
	}
	if err := deleteUserNodeOrderRefs(tx, userNodeTypeForward, forwardIDs); err != nil {
		return err
	}
	removed := make(map[uint]bool, len(forwardIDs))
	for _, id := range forwardIDs {
		removed[id] = true
	}
	var users []model.User
	if err := tx.Find(&users).Error; err != nil {
		return err
	}
	for i := range users {
		user := &users[i]
		if len(user.ForwardIDs) == 0 {
			continue
		}
		kept := make([]uint, 0, len(user.ForwardIDs))
		changed := false
		for _, id := range user.ForwardIDs {
			if removed[id] {
				changed = true
				continue
			}
			kept = append(kept, id)
		}
		if !changed {
			continue
		}
		user.ForwardIDs = normalizedIDs(kept)
		if err := tx.Model(user).Select("ForwardIDs", "UpdatedAt").Updates(user).Error; err != nil {
			return err
		}
	}
	return nil
}

func containsAnyID(ids []uint, targets map[uint]bool) bool {
	for _, id := range ids {
		if targets[id] {
			return true
		}
	}
	return false
}

func filterOutIDs(ids []uint, exclude map[uint]bool) []uint {
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if !exclude[id] {
			out = append(out, id)
		}
	}
	return normalizedIDs(out)
}

func filterIDsBySet(ids []uint, valid map[uint]bool) []uint {
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if valid[id] {
			out = append(out, id)
		}
	}
	return normalizedIDs(out)
}

func syncRemainingExplicitInboundAccess(tx *gorm.DB, user *model.User, candidateServers []uint, exclude map[uint]bool, filterByServers bool) error {
	primary := filterOutIDs(user.InboundIDs, exclude)
	ipv6 := filterOutIDs(user.InboundIPv6IDs, exclude)
	union := normalizedIDs(append(append([]uint(nil), primary...), ipv6...))
	var inbounds []model.Inbound
	if len(union) > 0 && (!filterByServers || len(candidateServers) > 0) {
		query := tx.Select("id", "server_id").Where("id IN ?", union)
		if filterByServers {
			query = query.Where("server_id IN ?", candidateServers)
		}
		if err := query.Find(&inbounds).Error; err != nil {
			return err
		}
	}
	validIDs := make(map[uint]bool, len(inbounds))
	serverIDs := make([]uint, 0, len(inbounds))
	for _, inbound := range inbounds {
		validIDs[inbound.ID] = true
		serverIDs = append(serverIDs, inbound.ServerID)
	}
	user.InboundIDs = filterIDsBySet(primary, validIDs)
	user.InboundIPv6IDs = filterIDsBySet(ipv6, validIDs)
	user.ServerIDs = normalizedIDs(serverIDs)
	return nil
}
