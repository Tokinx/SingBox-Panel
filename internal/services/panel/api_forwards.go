package panel

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
	"gorm.io/gorm"
)

type forwardReq struct {
	Tag             *string `json:"tag"`
	ListenPort      *int    `json:"listen_port"`
	Network         *string `json:"network"`
	TargetType      *string `json:"target_type"`
	TargetInboundID *uint   `json:"target_inbound_id"`
	TargetAddress   *string `json:"target_address"`
	TargetPort      *int    `json:"target_port"`
	Outbound        *string `json:"outbound"`
	Remark          *string `json:"remark"`
	Enabled         *bool   `json:"enabled"`
}

func (a *App) listForwards(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	var rows []model.PortForward
	if err := a.db.Where("server_id = ?", id).Order("id").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := hydratePortForwards(a.db, rows); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"forwards": rows})
}

func defaultForwardTag(port int) string {
	return fmt.Sprintf("fwd-%d", port)
}

func (a *App) applyForwardReq(db *gorm.DB, serverID uint, fwd *model.PortForward, req forwardReq, isCreate bool) error {
	if req.ListenPort != nil {
		fwd.ListenPort = *req.ListenPort
	}
	if isCreate && fwd.ListenPort == 0 {
		return fmt.Errorf("监听端口必填")
	}

	if req.Tag != nil {
		fwd.Tag = strings.TrimSpace(*req.Tag)
	}
	if isCreate && fwd.Tag == "" {
		fwd.Tag = defaultForwardTag(fwd.ListenPort)
	}

	if req.Network != nil {
		fwd.Network = *req.Network
	}
	netProto, err := singbox.NormalizeForwardNetwork(fwd.Network)
	if err != nil {
		return fmt.Errorf("转发协议只能是 TCP/UDP（留空）、tcp 或 udp")
	}
	fwd.Network = netProto

	if req.TargetType != nil {
		fwd.TargetType = strings.TrimSpace(*req.TargetType)
	}
	if fwd.TargetType == "" {
		if req.TargetInboundID != nil && *req.TargetInboundID > 0 {
			fwd.TargetType = model.ForwardTargetInbound
		} else {
			fwd.TargetType = model.ForwardTargetCustom
		}
	}

	if fwd.TargetType == model.ForwardTargetInbound {
		if req.TargetInboundID != nil {
			fwd.TargetInboundID = *req.TargetInboundID
		}
		fwd.TargetAddress = ""
		fwd.TargetPort = 0
	} else {
		if req.TargetAddress != nil {
			fwd.TargetAddress = strings.TrimSpace(*req.TargetAddress)
		}
		if req.TargetPort != nil {
			fwd.TargetPort = *req.TargetPort
		}
		fwd.TargetServerID = 0
		fwd.TargetInboundID = 0
	}

	if req.Outbound != nil {
		fwd.Outbound = *req.Outbound
	}
	ob, err := a.validateForwardOutbound(db, serverID, fwd.Outbound)
	if err != nil {
		return err
	}
	fwd.Outbound = ob

	if req.Remark != nil {
		fwd.Remark = strings.TrimSpace(*req.Remark)
	}
	if req.Enabled != nil {
		fwd.Enabled = *req.Enabled
	} else if isCreate {
		fwd.Enabled = true
	}

	if err := a.validateForwardTagAndPort(db, serverID, fwd.ID, fwd.Tag, fwd.ListenPort); err != nil {
		return err
	}

	// Only require the referenced target inbound to be currently enabled when this forward is enabled,
	// or when the target itself was newly set/created.
	requireEnabledTarget := fwd.Enabled || isCreate || req.TargetInboundID != nil || req.TargetType != nil
	_, _, targetSrv, targetIb, err := resolveForwardTarget(db, serverID, fwd, requireEnabledTarget)
	if err != nil {
		return err
	}
	if targetSrv != nil && targetIb != nil {
		fwd.TargetServerID = targetSrv.ID
		fwd.TargetInboundID = targetIb.ID
	}
	return nil
}

func (a *App) createForward(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	unlockOperation := a.lockServerOperation(id)
	defer unlockOperation()
	if !a.requireManagedMode(c, id) {
		return
	}
	var req forwardReq
	if !bindJSON(c, &req) {
		return
	}

	fwd := &model.PortForward{ServerID: id}
	if err := a.applyForwardReq(a.db, id, fwd, req, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(fwd).Error; err != nil {
			return err
		}
		return rememberForwardTrafficTag(tx, id, fwd.ID, fwd.Tag)
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	single := []model.PortForward{*fwd}
	_ = hydratePortForwards(a.db, single)
	*fwd = single[0]
	a.applyConfigAndRespond(c, id, gin.H{"forward": fwd})
}

func (a *App) updateForward(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	fwdID, ok := uintParam(c, "forwardID")
	if !ok {
		return
	}
	unlockOperation := a.lockServerOperation(id)
	defer unlockOperation()
	if !a.requireManagedMode(c, id) {
		return
	}

	var fwd model.PortForward
	if err := a.db.Where("id = ? AND server_id = ?", fwdID, id).First(&fwd).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "port forward not found"})
		return
	}

	previousOutbound := fwd.Outbound
	var req forwardReq
	if !bindJSON(c, &req) {
		return
	}
	if err := a.applyForwardReq(a.db, id, &fwd, req, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := rememberForwardTrafficTag(tx, id, fwd.ID, fwd.Tag); err != nil {
			return err
		}
		return tx.Save(&fwd).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	single := []model.PortForward{fwd}
	_ = hydratePortForwards(a.db, single)
	fwd = single[0]
	if previousOutbound != fwd.Outbound {
		a.refreshManagedRoutingTags(previousOutbound)
	}
	a.applyConfigAndRespond(c, id, gin.H{"forward": fwd})
}

func (a *App) deleteForward(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	fwdID, ok := uintParam(c, "forwardID")
	if !ok {
		return
	}
	unlockOperation := a.lockServerOperation(id)
	defer unlockOperation()
	if !a.requireManagedMode(c, id) {
		return
	}

	var fwd model.PortForward
	if err := a.db.Where("id = ? AND server_id = ?", fwdID, id).First(&fwd).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "port forward not found"})
		return
	}
	if err := a.db.Delete(&fwd).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	a.refreshManagedRoutingTags(fwd.Outbound)
	a.applyConfigAndRespond(c, id, gin.H{"ok": true})
}
