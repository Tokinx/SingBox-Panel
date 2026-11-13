package panel

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

type nodeExportUser struct {
	ID    uint   `json:"id"`
	Email string `json:"email"`
}

// Multi-user inbounds require an active, granted account: there is no shared
// credential to export. Fixed-credential nodes remain available without one.
func (a *App) serverNodeFormats(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	var srv model.Server
	if err := a.db.Preload("Inbounds").Preload("Forwards", func(db *gorm.DB) *gorm.DB {
		return db.Order("id")
	}).First(&srv, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	host := srv.Address
	if host == "" {
		host = srv.PublicIP
	}
	host, err := normalizeNodeAddress(host)
	if err != nil || host == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "节点地址未配置或无效"})
		return
	}

	var userID uint
	if raw, present := c.GetQuery("user_id"); present {
		parsed, err := strconv.ParseUint(raw, 10, strconv.IntSize)
		if err != nil || parsed == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户 ID"})
			return
		}
		userID = uint(parsed)
	}

	var multiInbounds []uint
	for _, ib := range srv.Inbounds {
		if !ib.Enabled {
			continue
		}
		st, _, _, err := buildInboundNodeSettings(&model.User{}, ib)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}
		if st.UseMultiUser(string(ib.Type)) {
			multiInbounds = append(multiInbounds, ib.ID)
		}
	}

	var multiForwardIDs []uint
	var targetInboundIDs []uint
	for _, fwd := range srv.Forwards {
		if fwd.Enabled && fwd.TargetType == model.ForwardTargetInbound && fwd.TargetInboundID > 0 {
			targetInboundIDs = append(targetInboundIDs, fwd.TargetInboundID)
		}
	}
	if len(targetInboundIDs) > 0 {
		var targetInbounds []model.Inbound
		if err := a.db.Where("id IN ? AND enabled = ?", normalizedIDs(targetInboundIDs), true).Find(&targetInbounds).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加载转发目标入站失败"})
			return
		}
		for _, tib := range targetInbounds {
			st, _, _, err := buildInboundNodeSettings(&model.User{}, tib)
			if err != nil {
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
				return
			}
			if st.UseMultiUser(string(tib.Type)) {
				for _, fwd := range srv.Forwards {
					if fwd.Enabled && fwd.TargetType == model.ForwardTargetInbound && fwd.TargetInboundID == tib.ID {
						multiForwardIDs = append(multiForwardIDs, fwd.ID)
					}
				}
			}
		}
	}

	choices := make([]nodeExportUser, 0)
	var selected *model.User
	if len(multiInbounds) > 0 || len(multiForwardIDs) > 0 {
		var users []model.User
		if err := a.db.Where("enabled = ?", true).Order("id").Find(&users).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加载导出用户失败"})
			return
		}
		now := time.Now()
		for i := range users {
			u := &users[i]
			if u.Expired(now) {
				continue
			}
			granted := false
			for _, inboundID := range multiInbounds {
				if u.HasInbound(srv.ID, inboundID) {
					granted = true
					break
				}
			}
			if !granted {
				for _, fwdID := range multiForwardIDs {
					if u.HasForward(fwdID) {
						granted = true
						break
					}
				}
			}
			if !granted {
				continue
			}
			choices = append(choices, nodeExportUser{ID: u.ID, Email: u.Email})
			if u.ID == userID {
				selected = u
			}
		}
	}
	if userID != 0 && selected == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "该用户不可用或未获授权使用此服务器的多用户入站"})
		return
	}

	var nodes []node
	for _, ib := range srv.Inbounds {
		if !ib.Enabled {
			continue
		}
		st, identity, ok, err := buildInboundNodeSettings(selected, ib)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}
		if st.UseMultiUser(string(ib.Type)) {
			if selected == nil || !selected.HasInbound(srv.ID, ib.ID) || !ok {
				continue
			}
		} else if !ok {
			continue
		}
		nodes = append(nodes, node{
			tag:      ib.Tag,
			server:   host,
			port:     ib.ListenPort,
			typ:      string(ib.Type),
			settings: st,
			user:     identity,
		})
	}
	nameServerNodes(srv.Name, nodes)

	fwdNodes, err := buildForwardedNodes(a.db, &srv, host, srv.Forwards, selected, false)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	nodes = append(nodes, fwdNodes...)

	var uriLines []string
	for _, n := range nodes {
		if link, err := singbox.BuildShareLink(n.clientNode()); err == nil {
			uriLines = append(uriLines, link)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"users":            choices,
		"multi_user_count": len(multiInbounds) + len(multiForwardIDs),
		"user_id":          userID,
		"items":            buildNodeFormatItems(nodes),
		"uri":              strings.Join(uriLines, "\n"),
		"clash":            clashProxiesYAML(nodes),
		"surge":            surgeLines(nodes),
	})
}
