package panel

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var mongoUserCountsCache struct {
	sync.Mutex
	mongo                *paneldb.MongoDB
	at                   time.Time
	all, active, blocked int64
}

// The paged UI must query a bounded page instead of materializing LoadStore.
func (a *App) handleMongoUsersPage(w http.ResponseWriter, r *http.Request, m *paneldb.MongoDB) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 200 {
		jsonError(w, "limit must be between 1 and 200", http.StatusBadRequest)
		return
	}
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		jsonError(w, "offset must be non-negative", http.StatusBadRequest)
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "all" && status != "active" && status != "blocked" {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	users := m.Database().Collection("users")
	active := bson.M{"is_deactivated": bson.M{"$ne": true}, "$and": bson.A{
		bson.M{"$or": bson.A{bson.M{"expires_at": bson.M{"$lte": 0}}, bson.M{"expires_at": bson.M{"$gt": time.Now().Unix()}}, bson.M{"expires_at": bson.M{"$exists": false}}}},
		bson.M{"$expr": bson.M{"$or": bson.A{
			bson.M{"$lte": bson.A{bson.M{"$ifNull": bson.A{"$total_bytes", 0}}, 0}},
			bson.M{"$lt": bson.A{bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$up_bytes", 0}}, bson.M{"$ifNull": bson.A{"$down_bytes", 0}}}}, "$total_bytes"}},
		}}},
	}}
	filter := bson.M{}
	if status == "active" {
		filter = active
	} else if status == "blocked" {
		filter = bson.M{"is_deactivated": true}
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		if len(q) > 256 {
			jsonError(w, "query too long", http.StatusBadRequest)
			return
		}
		re := bson.Regex{Pattern: regexp.QuoteMeta(q), Options: "i"}
		search := bson.M{"$or": bson.A{bson.M{"_id": re}, bson.M{"comment": re}, bson.M{"vk_hash": re}, bson.M{"device_id": re}, bson.M{"device_ids": re}}}
		filter = bson.M{"$and": bson.A{filter, search}}
	}
	total, err := users.CountDocuments(ctx, filter)
	if err != nil {
		jsonError(w, "users count: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	mongoUserCountsCache.Lock()
	if mongoUserCountsCache.mongo != m || time.Since(mongoUserCountsCache.at) > 30*time.Second {
		all, countErr := users.CountDocuments(ctx, bson.M{})
		if countErr == nil {
			var activeCount, blocked int64
			activeCount, countErr = users.CountDocuments(ctx, active)
			if countErr == nil {
				blocked, countErr = users.CountDocuments(ctx, bson.M{"is_deactivated": true})
			}
			if countErr == nil {
				mongoUserCountsCache.mongo, mongoUserCountsCache.at = m, time.Now()
				mongoUserCountsCache.all, mongoUserCountsCache.active, mongoUserCountsCache.blocked = all, activeCount, blocked
			}
		}
		if countErr != nil {
			mongoUserCountsCache.Unlock()
			jsonError(w, "users count: "+countErr.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	all, activeCount, blocked := mongoUserCountsCache.all, mongoUserCountsCache.active, mongoUserCountsCache.blocked
	mongoUserCountsCache.Unlock()
	var global paneldb.MongoGlobalDoc
	_ = m.Database().Collection("global").FindOne(ctx, bson.M{"_id": 1}).Decode(&global)
	cur, err := users.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetSkip(int64(offset)).SetLimit(int64(limit)))
	if err != nil {
		jsonError(w, "users page: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer cur.Close(ctx)
	inbound, _ := loadWdttInbound()
	host := a.resolveLinkHost(inbound)
	stats := loadServerStats()
	db := &PasswordsDB{MainPassword: global.MainPassword, Passwords: make(map[string]*PasswordEntry)}
	rows := make([]map[string]interface{}, 0, limit)
	for cur.Next(ctx) {
		var doc paneldb.MongoUserDoc
		if err := cur.Decode(&doc); err != nil {
			jsonError(w, "users page: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		entry := &PasswordEntry{DeviceID: doc.DeviceID, DeviceIDs: doc.DeviceIDs, MaxDevices: doc.MaxDevices, ExpiresAt: doc.ExpiresAt, DownBytes: doc.DownBytes, UpBytes: doc.UpBytes, TotalBytes: doc.TotalBytes, MaxDownMBps: doc.MaxDownMBps, MaxUpMBps: doc.MaxUpMBps, IsDeactivated: doc.IsDeactivated, Comment: doc.Comment, Ports: doc.Ports, VkHash: doc.VkHash, SubID: doc.SubID, LastSeenAt: doc.LastSeenAt, NodeOnlineAt: doc.NodeOnlineAt}
		db.Passwords[doc.Password] = entry
		if doc.Password == db.MainPassword {
			row := mainUserRow(db, stats, inbound, host, a.cfg.SubTitle, a.buildSubURL(entry.SubID))
			a.enrichUserSub(row, entry)
			rows = append(rows, row)
			continue
		}
		normalizeEntryDevices(entry)
		dtls, wg, client := resolveUserPorts(entry, inbound)
		rows = append(rows, map[string]interface{}{
			"password": maskPassword(doc.Password), "password_key": doc.Password, "device_id": deviceIDsDisplay(entry), "device_ids": entry.DeviceIDs,
			"devices_bound": len(entry.DeviceIDs), "max_devices": entryMaxDevices(db, doc.Password, entry), "comment": entry.Comment,
			"expires_at": entry.ExpiresAt, "expires": passwordExpiry(entry), "up": formatBytes(entry.UpBytes), "down": formatBytes(entry.DownBytes),
			"up_bytes": entry.UpBytes, "down_bytes": entry.DownBytes, "total_bytes": entry.TotalBytes, "total_gb": bytesToGB(entry.TotalBytes),
			"max_down_mbps": entry.MaxDownMBps, "max_up_mbps": entry.MaxUpMBps, "traffic_used": trafficUsed(entry), "traffic_used_fmt": formatBytes(trafficUsed(entry)),
			"traffic_exceeded": trafficExceeded(entry), "is_expired": isPasswordExpired(entry), "is_deactivated": entry.IsDeactivated,
			"active": !entry.IsDeactivated && !isPasswordExpired(entry) && !trafficExceeded(entry),
			"online": userOnline(doc.Password, deviceIDsDisplay(entry), false, stats, entry.NodeOnlineAt), "last_seen_at": entry.LastSeenAt,
			"ports": entry.Ports, "dtls_port": dtls, "wg_port": wg, "client_port": client, "vk_hash": entry.VkHash,
			"sub_id": entry.SubID, "sub_url": a.buildSubURL(entry.SubID), "link": buildWdttLink(host, doc.Password, a.cfg.SubTitle, entry.VkHash, entry, inbound, a.buildSubURL(entry.SubID)),
		})
	}
	if err := cur.Err(); err != nil {
		jsonError(w, fmt.Sprintf("users page: %v", err), http.StatusServiceUnavailable)
		return
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		jsonError(w, "nodes: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	filtered := make([]*paneldb.Node, 0, len(nodes))
	for _, node := range nodes {
		if node != nil && node.IsActive && (!node.IsLocal || localNodeEnabled()) {
			filtered = append(filtered, node)
		}
	}
	jsonOK(w, map[string]interface{}{
		"users": rows, "total": total, "counts": map[string]int64{"all": all, "active": activeCount, "blocked": blocked}, "nodes": filtered,
		"inbound": map[string]interface{}{"tag": inbound.Tag, "sub_title": a.cfg.SubTitle, "remark": inbound.Remark, "listen_host": inbound.ListenHost, "server_host": inbound.ServerHost, "default_link_host": a.defaultLinkHost(), "panel_tls": panelTLSEnabled(a.cfg), "dtls_port": inbound.DtlsPort, "wg_port": inbound.WgPort, "client_port": inbound.ClientPort, "dns": inbound.DNS, "max_users": inbound.MaxUsers},
	})
}
