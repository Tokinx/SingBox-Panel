package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errForwardTrafficTagReserved = errors.New("历史端口转发标签已保留，不能由其他入站或端口转发复用，请使用新标签")

func forwardTagKey(tag string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(tag)))
	return hex.EncodeToString(sum[:])
}

func checkForwardTrafficTag(db *gorm.DB, serverID, forwardID uint, tag string) error {
	key := forwardTagKey(tag)
	var fwdIdentity model.ForwardTrafficIdentity
	err := db.Where("server_id = ? AND tag_key = ?", serverID, key).First(&fwdIdentity).Error
	if err == nil {
		if fwdIdentity.ForwardID != forwardID {
			return fmt.Errorf("%w: %q", errForwardTrafficTagReserved, tag)
		}
	} else if err != gorm.ErrRecordNotFound {
		return err
	}

	var ibIdentity model.InboundTrafficIdentity
	err = db.Where("server_id = ? AND tag_key = ?", serverID, key).First(&ibIdentity).Error
	if err == nil {
		return fmt.Errorf("%w: %q", errInboundTrafficTagReserved, tag)
	} else if err != gorm.ErrRecordNotFound {
		return err
	}
	return nil
}

// validateImportedForwardTrafficTags rejects any raw or imported inbound tag
// that collides with historical forward traffic identities, and prevents
// imported forwards from reusing tags owned by another forward or inbound.
func validateImportedForwardTrafficTags(db *gorm.DB, serverID uint, raw []byte, parsedForwards []singbox.ForwardInput) error {
	var document struct {
		Inbounds []struct {
			Tag string `json:"tag"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return err
	}
	var forwards []model.PortForward
	if err := db.Select("id", "tag").Where("server_id = ?", serverID).Find(&forwards).Error; err != nil {
		return err
	}
	fwdByTag := make(map[string]uint, len(forwards))
	for _, f := range forwards {
		fwdByTag[f.Tag] = f.ID
	}
	parsedFwdSet := make(map[string]bool, len(parsedForwards))
	for _, f := range parsedForwards {
		parsedFwdSet[f.Tag] = true
	}
	for _, ib := range document.Inbounds {
		tag := strings.TrimSpace(ib.Tag)
		if tag == "" {
			continue
		}
		if parsedFwdSet[tag] {
			if err := checkForwardTrafficTag(db, serverID, fwdByTag[tag], tag); err != nil {
				return err
			}
			continue
		}
		var fwdIdentity model.ForwardTrafficIdentity
		err := db.Where("server_id = ? AND tag_key = ?", serverID, forwardTagKey(tag)).First(&fwdIdentity).Error
		if err == nil {
			return fmt.Errorf("%w: %q", errForwardTrafficTagReserved, tag)
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
	}
	return nil
}

func rememberForwardTrafficTag(db *gorm.DB, serverID, forwardID uint, tag string) error {
	if serverID == 0 || forwardID == 0 || strings.TrimSpace(tag) == "" {
		return nil
	}
	key := forwardTagKey(tag)
	row := model.ForwardTrafficIdentity{
		ServerID:  serverID,
		Tag:       tag,
		TagKey:    key,
		ForwardID: forwardID,
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "server_id"}, {Name: "tag_key"}},
		DoNothing: true,
	}).Create(&row).Error; err != nil {
		return err
	}
	var stored model.ForwardTrafficIdentity
	if err := db.Where("server_id = ? AND tag_key = ?", serverID, key).First(&stored).Error; err != nil {
		return err
	}
	if stored.ForwardID != forwardID {
		return fmt.Errorf("标签 %q 已被其他端口转发占用", tag)
	}
	return nil
}

func inboundEffectiveGuard(ib model.Inbound) (tag string, port int, enabled bool) {
	if len(ib.Settings) == 0 {
		return "", 0, false
	}
	var st singbox.InboundSettings
	if err := json.Unmarshal(ib.Settings, &st); err != nil || !st.RealityGuardEnabled() {
		return "", 0, false
	}
	guardTag := strings.TrimSpace(st.TLS.Reality.Guard.Tag)
	if guardTag == "" {
		guardTag = ib.Tag + "-guard"
	}
	return guardTag, st.TLS.Reality.Guard.InternalPort, true
}

func checkInboundConflictWithForwards(db *gorm.DB, serverID uint, inboundTag, guardTag string, ports ...int) error {
	var forwards []model.PortForward
	if err := db.Where("server_id = ?", serverID).Find(&forwards).Error; err != nil {
		return err
	}
	checkTags := make([]string, 0, 2)
	if inboundTag != "" {
		checkTags = append(checkTags, inboundTag)
	}
	if guardTag != "" {
		checkTags = append(checkTags, guardTag)
	}
	for _, f := range forwards {
		for _, t := range checkTags {
			if f.Tag == t {
				return fmt.Errorf("标签 %q 与端口转发 %q 冲突", t, f.Tag)
			}
		}
		for _, p := range ports {
			if p > 0 && f.ListenPort == p {
				return fmt.Errorf("端口 %d 已被端口转发 %q 占用", p, f.Tag)
			}
		}
	}
	for _, t := range checkTags {
		var fwdID model.ForwardTrafficIdentity
		err := db.Where("server_id = ? AND tag_key = ?", serverID, forwardTagKey(t)).First(&fwdID).Error
		if err == nil {
			return fmt.Errorf("标签 %q 曾用于该服务器的端口转发流量统计，请换一个标签", t)
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
	}
	return nil
}

type trafficForwardInfo struct {
	ForwardID uint
	Tag       string
	Port      int
	Network   string
}

func loadTrafficForwards(db *gorm.DB, serverID uint) (byTag map[string]trafficForwardInfo, byID map[uint]trafficForwardInfo, err error) {
	byTag = make(map[string]trafficForwardInfo)
	byID = make(map[uint]trafficForwardInfo)

	var forwards []model.PortForward
	if err = db.Where("server_id = ?", serverID).Order("id").Find(&forwards).Error; err != nil {
		return nil, nil, err
	}
	for _, f := range forwards {
		info := trafficForwardInfo{
			ForwardID: f.ID,
			Tag:       f.Tag,
			Port:      f.ListenPort,
			Network:   f.Network,
		}
		byTag[f.Tag] = info
		byID[f.ID] = info
	}

	var identities []model.ForwardTrafficIdentity
	if err = db.Where("server_id = ?", serverID).Order("id").Find(&identities).Error; err != nil {
		return nil, nil, err
	}
	for _, ident := range identities {
		if _, active := byTag[ident.Tag]; active {
			continue
		}
		if current, ok := byID[ident.ForwardID]; ok {
			byTag[ident.Tag] = trafficForwardInfo{
				ForwardID: ident.ForwardID,
				Tag:       ident.Tag,
				Port:      current.Port,
				Network:   current.Network,
			}
		} else {
			byTag[ident.Tag] = trafficForwardInfo{
				ForwardID: ident.ForwardID,
				Tag:       ident.Tag,
			}
		}
	}
	return byTag, byID, nil
}
