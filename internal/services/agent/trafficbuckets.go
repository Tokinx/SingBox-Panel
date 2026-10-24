package agent

import (
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
)

type collectedTrafficBucket struct {
	protocol.TrafficTimeBucket
	ports map[string]protocol.PortTrafficSnapshot
	users map[userTrafficKey]protocol.UserTrafficSnapshot
}

// Callers hold s.mu. Collection runs regardless of panel connectivity.
func (s *trafficSampler) bucket(now time.Time) *collectedTrafficBucket {
	if s.buckets == nil {
		s.buckets = make(map[int64]*collectedTrafficBucket)
	}
	key := now.UTC().Truncate(time.Minute).Unix()
	b := s.buckets[key]
	if b == nil {
		b = &collectedTrafficBucket{TrafficTimeBucket: protocol.TrafficTimeBucket{Time: key},
			ports: make(map[string]protocol.PortTrafficSnapshot), users: make(map[userTrafficKey]protocol.UserTrafficSnapshot)}
		s.buckets[key] = b
	}
	return b
}

func (s *trafficSampler) addPortBucket(now time.Time, inbound string, delta [2]uint64) {
	b := s.bucket(now)
	p := b.ports[inbound]
	p.Inbound = inbound
	p.Upload += delta[0]
	p.Download += delta[1]
	b.ports[inbound] = p
}

func (s *trafficSampler) addUserBucket(now time.Time, key userTrafficKey, delta [2]uint64) {
	b := s.bucket(now)
	u := b.users[key]
	u.Inbound, u.User = key.inbound, key.user
	u.Upload += delta[0]
	u.Download += delta[1]
	b.users[key] = u
}

// accountingSnapshot freezes one bounded batch, leaving newly collected
// bytes in separate buckets. Successful socket writes do not drain it: only
// the panel's post-commit receipt can do that, even after a reconnect.
func (s *trafficSampler) accountingSnapshot() *protocol.TrafficSnapshot {
	snapshot := s.snapshot()
	if snapshot == nil {
		snapshot = &protocol.TrafficSnapshot{Unavailable: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.report == nil && len(s.buckets) > 0 {
		if s.accountingSession == "" {
			s.accountingSession = uuid.NewString()
		}
		keys := make([]int64, 0, len(s.buckets))
		for key := range s.buckets {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		if len(keys) > 128 {
			keys = keys[:128]
		}
		s.reportSequence++
		s.report = &protocol.TrafficReport{Session: s.accountingSession, Sequence: s.reportSequence}
		entries := 0
		for _, key := range keys {
			b := s.buckets[key]
			out := b.TrafficTimeBucket
			for tag, p := range b.ports {
				if entries >= 4096 {
					break
				}
				out.Ports = append(out.Ports, p)
				delete(b.ports, tag)
				entries++
			}
			for key, u := range b.users {
				if entries >= 4096 {
					break
				}
				out.Users = append(out.Users, u)
				delete(b.users, key)
				entries++
			}
			sort.Slice(out.Ports, func(i, j int) bool { return out.Ports[i].Inbound < out.Ports[j].Inbound })
			sort.Slice(out.Users, func(i, j int) bool {
				if out.Users[i].Inbound != out.Users[j].Inbound {
					return out.Users[i].Inbound < out.Users[j].Inbound
				}
				return out.Users[i].User < out.Users[j].User
			})
			s.report.Buckets = append(s.report.Buckets, out)
			// A very busy minute can span multiple reports. Its node total is
			// emitted once; remaining port/user rows keep the same UTC timestamp.
			b.TrafficTimeBucket = protocol.TrafficTimeBucket{Time: key}
			if len(b.ports) == 0 && len(b.users) == 0 {
				delete(s.buckets, key)
			}
			if entries >= 4096 {
				break
			}
		}
		// These aggregate mirrors are retained for rate-window bookkeeping;
		// the immutable report above now owns all timestamped accounting bytes.
		clear(s.pendingPorts)
		clear(s.pendingUsers)
		s.peakUpload, s.peakDownload = 0, 0
	}
	snapshot.AccountingVersion = 2
	snapshot.Report = s.report
	snapshot.Ports, snapshot.Users = nil, nil
	return snapshot
}

func (s *trafficSampler) acknowledgeReport(ack protocol.TrafficAck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.report != nil && s.report.Session == ack.Session && s.report.Sequence == ack.Sequence {
		s.report = nil
	}
}
