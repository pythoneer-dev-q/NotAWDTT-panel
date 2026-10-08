package paneldb

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	DefaultMongoURI = "mongodb://127.0.0.1:27017/wdtt"
	MongoConfigFile = "/etc/wdtt/mongodb.uri"
	DefaultMongoDB  = "wdtt"
)

// Node - VPS-нода для мультисерверной генерации и работы
type Node struct {
	ID          string `bson:"_id" json:"id"`
	Name        string `bson:"name" json:"name"`
	Host        string `bson:"host" json:"host"`
	DtlsPort    int    `bson:"dtls_port" json:"dtls_port"`
	RawPort     int    `bson:"raw_port" json:"raw_port"`
	ApiKey      string `bson:"api_key" json:"api_key"`
	IsActive    bool   `bson:"is_active" json:"is_active"`
	IsLocal     bool   `bson:"is_local" json:"is_local"`
	LastSeenAt  int64  `bson:"last_seen_at" json:"last_seen_at"`
	Status      string `bson:"status" json:"status"` // "online", "offline"
	OnlineUsers int    `bson:"online_users" json:"online_users"`
	TrafficUp   int64  `bson:"traffic_up" json:"traffic_up"`
	TrafficDown int64  `bson:"traffic_down" json:"traffic_down"`
	CreatedAt   int64  `bson:"created_at" json:"created_at"`
}

type MongoGlobalDoc struct {
	ID           int    `bson:"_id" json:"id"`
	MainPassword string `bson:"main_password" json:"main_password"`
	AdminID      string `bson:"admin_id" json:"admin_id"`
	BotToken     string `bson:"bot_token" json:"bot_token"`
}

type MongoInboundDoc struct {
	ID                  int    `bson:"_id" json:"id"`
	Tag                 string `bson:"tag" json:"tag"`
	Remark              string `bson:"remark" json:"remark"`
	Enable              bool   `bson:"enable" json:"enable"`
	ListenHost          string `bson:"listen_host" json:"listen_host"`
	ServerHost          string `bson:"server_host" json:"server_host"`
	DtlsPort            int    `bson:"dtls_port" json:"dtls_port"`
	WgPort              int    `bson:"wg_port" json:"wg_port"`
	ClientPort          int    `bson:"client_port" json:"client_port"`
	DNS                 string `bson:"dns" json:"dns"`
	MTU                 int    `bson:"mtu" json:"mtu"`
	MaxUsers            int    `bson:"max_users" json:"max_users"`
	HandshakeTimeoutSec int    `bson:"handshake_timeout_sec" json:"handshake_timeout_sec"`
	MaxDtlsPerDevice    int    `bson:"max_dtls_per_device" json:"max_dtls_per_device"`
	OnlineTimeoutSec    int    `bson:"online_timeout_sec" json:"online_timeout_sec"`
	WgKeepaliveSec      int    `bson:"wg_keepalive_sec" json:"wg_keepalive_sec"`
	StatsIntervalSec    int    `bson:"stats_interval_sec" json:"stats_interval_sec"`
	AdminAddr           string `bson:"admin_addr" json:"admin_addr"`
	RawEnable           bool   `bson:"raw_enable" json:"raw_enable"`
	RawDirectPort       int    `bson:"raw_direct_port" json:"raw_direct_port"`
}

type MongoUserDoc struct {
	Password      string   `bson:"_id" json:"password"`
	DeviceID      string   `bson:"device_id" json:"device_id"`
	DeviceIDs     []string `bson:"device_ids" json:"device_ids"`
	MaxDevices    int      `bson:"max_devices" json:"max_devices"`
	ExpiresAt     int64    `bson:"expires_at" json:"expires_at"`
	DownBytes     int64    `bson:"down_bytes" json:"down_bytes"`
	UpBytes       int64    `bson:"up_bytes" json:"up_bytes"`
	TotalBytes    int64    `bson:"total_bytes" json:"total_bytes"`
	MaxDownMBps   float64  `bson:"max_down_mbps" json:"max_down_mbps"`
	MaxUpMBps     float64  `bson:"max_up_mbps" json:"max_up_mbps"`
	IsDeactivated bool     `bson:"is_deactivated" json:"is_deactivated"`
	Comment       string   `bson:"comment" json:"comment"`
	Ports         string   `bson:"ports" json:"ports"`
	VkHash        string   `bson:"vk_hash" json:"vk_hash"`
	WbRoom        string   `bson:"wb_room" json:"wb_room"`
	SubID         string   `bson:"sub_id" json:"sub_id"`
	LastSeenAt    int64    `bson:"last_seen_at" json:"last_seen_at"`
}

type MongoDeviceDoc struct {
	DeviceID string `bson:"_id" json:"device_id"`
	IP       string `bson:"ip" json:"ip"`
	PrivKey  string `bson:"priv_key" json:"priv_key"`
	PubKey   string `bson:"pub_key" json:"pub_key"`
}

type MongoDB struct {
	client   *mongo.Client
	db       *mongo.Database
	dbName   string
	uri      string
	closed   bool
	closedMu sync.Mutex
}

var (
	defaultMongo     *MongoDB
	defaultMongoOnce sync.Once
	defaultMongoErr  error
)

// ResolveMongoURI проверяет MONGODB_URI, файл конфигурации и fallback.
func ResolveMongoURI() string {
	if uri := os.Getenv("MONGODB_URI"); strings.TrimSpace(uri) != "" {
		return strings.TrimSpace(uri)
	}
	if data, err := os.ReadFile(MongoConfigFile); err == nil {
		uri := strings.TrimSpace(string(data))
		if uri != "" {
			return uri
		}
	}
	return DefaultMongoURI
}

// ConnectMongo подключается к MongoDB.
func ConnectMongo(ctx context.Context, uri string) (*MongoDB, error) {
	if strings.TrimSpace(uri) == "" {
		uri = ResolveMongoURI()
	}

	opts := options.Client().ApplyURI(uri).SetTimeout(5 * time.Second)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect error: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping failed (%s): %w", uri, err)
	}

	dbName := DefaultMongoDB
	// Извлекаем имя базы из URI если указано
	if idx := strings.LastIndex(uri, "/"); idx != -1 {
		rest := uri[idx+1:]
		if q := strings.Index(rest, "?"); q != -1 {
			rest = rest[:q]
		}
		if rest != "" {
			dbName = rest
		}
	}

	mdb := &MongoDB{
		client: client,
		db:     client.Database(dbName),
		dbName: dbName,
		uri:    uri,
	}

	// Инициализация дефолтной локальной ноды если ещё нет нод
	_ = mdb.ensureLocalNode()

	return mdb, nil
}

// GetDefaultMongo возвращает синглтон подключения к MongoDB.
func GetDefaultMongo() (*MongoDB, error) {
	defaultMongoOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		defaultMongo, defaultMongoErr = ConnectMongo(ctx, "")
	})
	return defaultMongo, defaultMongoErr
}

// SetDefaultMongo устанавливает инстанс MongoDB (например, в тестах или инициализации).
func SetDefaultMongo(m *MongoDB) {
	defaultMongo = m
}

// Close закрывает подключение к MongoDB.
func (m *MongoDB) Close() error {
	m.closedMu.Lock()
	defer m.closedMu.Unlock()
	if m.closed || m.client == nil {
		return nil
	}
	m.closed = true
	return m.client.Disconnect(context.Background())
}

func (m *MongoDB) Database() *mongo.Database {
	return m.db
}

// ensureLocalNode проверяет наличие нод и добавляет локальную ноду (Master) по умолчанию.
func (m *MongoDB) ensureLocalNode() error {
	coll := m.db.Collection("nodes")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	count, err := coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}
	if count == 0 {
		local := Node{
			ID:          "local",
			Name:        "Основной VPS (Master)",
			Host:        "",
			DtlsPort:    56000,
			RawPort:     56003,
			ApiKey:      "local-master-key",
			IsActive:    true,
			IsLocal:     true,
			LastSeenAt:  time.Now().Unix(),
			Status:      "online",
			OnlineUsers: 0,
			CreatedAt:   time.Now().Unix(),
		}
		_, err = coll.InsertOne(ctx, local)
		if err != nil {
			log.Printf("[MONGO] init local node: %v", err)
		}
	}
	return nil
}

// ----------------- Nodes CRUD -----------------

func (m *MongoDB) ListNodes(ctx context.Context) ([]*Node, error) {
	coll := m.db.Collection("nodes")
	cursor, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var list []*Node
	for cursor.Next(ctx) {
		var n Node
		if err := cursor.Decode(&n); err == nil {
			list = append(list, &n)
		}
	}
	return list, nil
}

func (m *MongoDB) GetNode(ctx context.Context, id string) (*Node, error) {
	coll := m.db.Collection("nodes")
	var n Node
	err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (m *MongoDB) SaveNode(ctx context.Context, n *Node) error {
	if n == nil || n.ID == "" {
		return fmt.Errorf("invalid node")
	}
	coll := m.db.Collection("nodes")
	opts := options.UpdateOne().SetUpsert(true)
	_, err := coll.UpdateOne(ctx, bson.M{"_id": n.ID}, bson.M{"$set": n}, opts)
	return err
}

func (m *MongoDB) DeleteNode(ctx context.Context, id string) error {
	if id == "" || id == "local" {
		return fmt.Errorf("cannot delete master node")
	}
	coll := m.db.Collection("nodes")
	_, err := coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (m *MongoDB) UpdateNodeHeartbeat(ctx context.Context, apiKey string, onlineUsers int, upDelta, downDelta int64) error {
	coll := m.db.Collection("nodes")
	now := time.Now().Unix()
	update := bson.M{
		"$set": bson.M{
			"last_seen_at":  now,
			"status":        "online",
			"online_users":  onlineUsers,
		},
		"$inc": bson.M{
			"traffic_up":   upDelta,
			"traffic_down": downDelta,
		},
	}
	res, err := coll.UpdateOne(ctx, bson.M{"api_key": apiKey}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("node not found for api_key")
	}
	return nil
}

// ----------------- Store CRUD -----------------

func (m *MongoDB) LoadStore(ctx context.Context) (*Store, error) {
	out := NewStore()

	// 1. Global
	var g MongoGlobalDoc
	if err := m.db.Collection("global").FindOne(ctx, bson.M{"_id": 1}).Decode(&g); err == nil {
		out.MainPassword = g.MainPassword
		out.AdminID = g.AdminID
		out.BotToken = g.BotToken
	}

	// 2. Users
	uCur, err := m.db.Collection("users").Find(ctx, bson.M{})
	if err == nil {
		defer uCur.Close(ctx)
		for uCur.Next(ctx) {
			var doc MongoUserDoc
			if err := uCur.Decode(&doc); err == nil {
				u := &User{
					DeviceID:      doc.DeviceID,
					DeviceIDs:     doc.DeviceIDs,
					MaxDevices:    doc.MaxDevices,
					ExpiresAt:     doc.ExpiresAt,
					DownBytes:     doc.DownBytes,
					UpBytes:       doc.UpBytes,
					TotalBytes:    doc.TotalBytes,
					MaxDownMBps:   doc.MaxDownMBps,
					MaxUpMBps:     doc.MaxUpMBps,
					IsDeactivated: doc.IsDeactivated,
					Comment:       doc.Comment,
					Ports:         doc.Ports,
					VkHash:        doc.VkHash,
					WbRoom:        doc.WbRoom,
					SubID:         doc.SubID,
					LastSeenAt:    doc.LastSeenAt,
				}
				NormalizeUser(u)
				out.Users[doc.Password] = u
			}
		}
	}

	// 3. Devices
	dCur, err := m.db.Collection("devices").Find(ctx, bson.M{})
	if err == nil {
		defer dCur.Close(ctx)
		for dCur.Next(ctx) {
			var doc MongoDeviceDoc
			if err := dCur.Decode(&doc); err == nil {
				out.Devices[doc.DeviceID] = &Device{
					DeviceID: doc.DeviceID,
					IP:       doc.IP,
					PrivKey:  doc.PrivKey,
					PubKey:   doc.PubKey,
				}
			}
		}
	}

	return out, nil
}

func (m *MongoDB) SaveStore(ctx context.Context, s *Store, opts SaveOptions) error {
	if s == nil {
		return fmt.Errorf("nil store")
	}

	// 1. Global
	gDoc := MongoGlobalDoc{
		ID:           1,
		MainPassword: s.MainPassword,
		AdminID:      s.AdminID,
		BotToken:     s.BotToken,
	}
	_, err := m.db.Collection("global").UpdateOne(ctx,
		bson.M{"_id": 1},
		bson.M{"$set": gDoc},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return err
	}

	// 2. Users
	existingSubIDs := make(map[string]string)
	if opts.PreserveSubIDs {
		cur, err := m.db.Collection("users").Find(ctx, bson.M{})
		if err == nil {
			defer cur.Close(ctx)
			for cur.Next(ctx) {
				var u MongoUserDoc
				if err := cur.Decode(&u); err == nil && u.SubID != "" {
					existingSubIDs[u.Password] = u.SubID
				}
			}
		}
	}

	// Delete users that no longer exist in src
	currentPasses := make([]string, 0, len(s.Users))
	for p := range s.Users {
		currentPasses = append(currentPasses, p)
	}
	_, _ = m.db.Collection("users").DeleteMany(ctx, bson.M{"_id": bson.M{"$nin": currentPasses}})

	// Upsert users
	for pass, u := range s.Users {
		if u == nil {
			continue
		}
		subID := u.SubID
		if subID == "" && opts.PreserveSubIDs {
			subID = existingSubIDs[pass]
		}
		doc := MongoUserDoc{
			Password:      pass,
			DeviceID:      u.DeviceID,
			DeviceIDs:     u.DeviceIDs,
			MaxDevices:    u.MaxDevices,
			ExpiresAt:     u.ExpiresAt,
			DownBytes:     u.DownBytes,
			UpBytes:       u.UpBytes,
			TotalBytes:    u.TotalBytes,
			MaxDownMBps:   u.MaxDownMBps,
			MaxUpMBps:     u.MaxUpMBps,
			IsDeactivated: u.IsDeactivated,
			Comment:       u.Comment,
			Ports:         u.Ports,
			VkHash:        u.VkHash,
			WbRoom:        u.WbRoom,
			SubID:         subID,
			LastSeenAt:    u.LastSeenAt,
		}
		_, err := m.db.Collection("users").UpdateOne(ctx,
			bson.M{"_id": pass},
			bson.M{"$set": doc},
			options.UpdateOne().SetUpsert(true),
		)
		if err != nil {
			return err
		}
	}

	// 3. Devices
	currentDevs := make([]string, 0, len(s.Devices))
	for did := range s.Devices {
		currentDevs = append(currentDevs, did)
	}
	_, _ = m.db.Collection("devices").DeleteMany(ctx, bson.M{"_id": bson.M{"$nin": currentDevs}})

	for did, d := range s.Devices {
		if d == nil {
			continue
		}
		dDoc := MongoDeviceDoc{
			DeviceID: did,
			IP:       d.IP,
			PrivKey:  d.PrivKey,
			PubKey:   d.PubKey,
		}
		_, err := m.db.Collection("devices").UpdateOne(ctx,
			bson.M{"_id": did},
			bson.M{"$set": dDoc},
			options.UpdateOne().SetUpsert(true),
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// ----------------- User Fine-Grained Ops -----------------

func (m *MongoDB) UpsertUser(ctx context.Context, pass string, u *User) error {
	if u == nil || pass == "" {
		return fmt.Errorf("invalid user")
	}
	doc := MongoUserDoc{
		Password:      pass,
		DeviceID:      u.DeviceID,
		DeviceIDs:     u.DeviceIDs,
		MaxDevices:    u.MaxDevices,
		ExpiresAt:     u.ExpiresAt,
		DownBytes:     u.DownBytes,
		UpBytes:       u.UpBytes,
		TotalBytes:    u.TotalBytes,
		MaxDownMBps:   u.MaxDownMBps,
		MaxUpMBps:     u.MaxUpMBps,
		IsDeactivated: u.IsDeactivated,
		Comment:       u.Comment,
		Ports:         u.Ports,
		VkHash:        u.VkHash,
		WbRoom:        u.WbRoom,
		SubID:         u.SubID,
		LastSeenAt:    u.LastSeenAt,
	}
	_, err := m.db.Collection("users").UpdateOne(ctx,
		bson.M{"_id": pass},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (m *MongoDB) DeleteUser(ctx context.Context, pass string) error {
	_, err := m.db.Collection("users").DeleteOne(ctx, bson.M{"_id": pass})
	return err
}

func (m *MongoDB) RenameUser(ctx context.Context, oldPass, newPass string, u *User) error {
	if oldPass == newPass {
		return m.UpsertUser(ctx, newPass, u)
	}
	if err := m.UpsertUser(ctx, newPass, u); err != nil {
		return err
	}
	return m.DeleteUser(ctx, oldPass)
}

func (m *MongoDB) UpdateLastSeen(ctx context.Context, pass string, ts int64) error {
	_, err := m.db.Collection("users").UpdateOne(ctx,
		bson.M{"_id": pass},
		bson.M{"$set": bson.M{"last_seen_at": ts}},
	)
	return err
}

func (m *MongoDB) UpdateLastSeenBatch(ctx context.Context, updates map[string]int64) error {
	for pass, ts := range updates {
		_ = m.UpdateLastSeen(ctx, pass, ts)
	}
	return nil
}

func (m *MongoDB) AddTraffic(ctx context.Context, pass string, downDelta, upDelta int64) error {
	_, err := m.db.Collection("users").UpdateOne(ctx,
		bson.M{"_id": pass},
		bson.M{"$inc": bson.M{
			"down_bytes": downDelta,
			"up_bytes":   upDelta,
		}},
	)
	return err
}

func (m *MongoDB) ResetUserTraffic(ctx context.Context, pass string) error {
	_, err := m.db.Collection("users").UpdateOne(ctx,
		bson.M{"_id": pass},
		bson.M{"$set": bson.M{
			"down_bytes": 0,
			"up_bytes":   0,
		}},
	)
	return err
}

// ----------------- Inbound & Config -----------------

func (m *MongoDB) LoadInbound(ctx context.Context) (*Inbound, error) {
	var doc MongoInboundDoc
	err := m.db.Collection("inbound").FindOne(ctx, bson.M{"_id": 1}).Decode(&doc)
	if err != nil {
		// Дефолтный inbound если ещё нет
		in := &Inbound{
			Tag:                 "wdtt-in",
			Remark:              "WDTT",
			Enable:              true,
			ListenHost:          "0.0.0.0",
			ServerHost:          "",
			DtlsPort:            56000,
			WgPort:              56001,
			ClientPort:          9000,
			DNS:                 "1.1.1.1",
			MTU:                 1280,
			MaxUsers:            100,
			HandshakeTimeoutSec: 30,
			MaxDtlsPerDevice:    0,
			OnlineTimeoutSec:    15,
			WgKeepaliveSec:      25,
			StatsIntervalSec:    2,
			AdminAddr:           "127.0.0.1:2861",
			RawEnable:           true,
			RawDirectPort:       56003,
		}
		_ = m.SaveInbound(ctx, in)
		return in, nil
	}
	return &Inbound{
		Tag:                 doc.Tag,
		Remark:              doc.Remark,
		Enable:              doc.Enable,
		ListenHost:          doc.ListenHost,
		ServerHost:          doc.ServerHost,
		DtlsPort:            doc.DtlsPort,
		WgPort:              doc.WgPort,
		ClientPort:          doc.ClientPort,
		DNS:                 doc.DNS,
		MTU:                 doc.MTU,
		MaxUsers:            doc.MaxUsers,
		HandshakeTimeoutSec: doc.HandshakeTimeoutSec,
		MaxDtlsPerDevice:    doc.MaxDtlsPerDevice,
		OnlineTimeoutSec:    doc.OnlineTimeoutSec,
		WgKeepaliveSec:      doc.WgKeepaliveSec,
		StatsIntervalSec:    doc.StatsIntervalSec,
		AdminAddr:           doc.AdminAddr,
		RawEnable:           doc.RawEnable,
		RawDirectPort:       doc.RawDirectPort,
	}, nil
}

func (m *MongoDB) SaveInbound(ctx context.Context, in *Inbound) error {
	if in == nil {
		return fmt.Errorf("nil inbound")
	}
	doc := MongoInboundDoc{
		ID:                  1,
		Tag:                 in.Tag,
		Remark:              in.Remark,
		Enable:              in.Enable,
		ListenHost:          in.ListenHost,
		ServerHost:          in.ServerHost,
		DtlsPort:            in.DtlsPort,
		WgPort:              in.WgPort,
		ClientPort:          in.ClientPort,
		DNS:                 in.DNS,
		MTU:                 in.MTU,
		MaxUsers:            in.MaxUsers,
		HandshakeTimeoutSec: in.HandshakeTimeoutSec,
		MaxDtlsPerDevice:    in.MaxDtlsPerDevice,
		OnlineTimeoutSec:    in.OnlineTimeoutSec,
		WgKeepaliveSec:      in.WgKeepaliveSec,
		StatsIntervalSec:    in.StatsIntervalSec,
		AdminAddr:           in.AdminAddr,
		RawEnable:           in.RawEnable,
		RawDirectPort:       in.RawDirectPort,
	}
	_, err := m.db.Collection("inbound").UpdateOne(ctx,
		bson.M{"_id": 1},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (m *MongoDB) LoadStartupSettings(ctx context.Context) (StartupSettings, bool, error) {
	in, err := m.LoadInbound(ctx)
	if err != nil {
		return StartupSettings{}, false, err
	}
	s := StartupSettings{
		ListenHost:    in.ListenHost,
		DtlsPort:      in.DtlsPort,
		WgPort:        in.WgPort,
		RawDirectPort: in.RawDirectPort,
		AdminAddr:     in.AdminAddr,
		Enable:        in.Enable,
		RuntimeSettings: RuntimeSettings{
			DNS:                 in.DNS,
			MTU:                 in.MTU,
			MaxUsers:            in.MaxUsers,
			HandshakeTimeoutSec: in.HandshakeTimeoutSec,
			MaxDtlsPerDevice:    in.MaxDtlsPerDevice,
			OnlineTimeoutSec:    in.OnlineTimeoutSec,
			WgKeepaliveSec:      in.WgKeepaliveSec,
			StatsIntervalSec:    in.StatsIntervalSec,
			RawEnable:           in.RawEnable,
		},
	}
	return s, true, nil
}

// ----------------- Panel Config -----------------

func (m *MongoDB) LoadPanelConfig(ctx context.Context) (*PanelConfig, error) {
	var cfg PanelConfig
	err := m.db.Collection("panel_config").FindOne(ctx, bson.M{"_id": 1}).Decode(&cfg)
	if err != nil {
		// Дефолтный конфиг панели
		def := DefaultPanelConfig()
		_ = m.SavePanelConfig(ctx, def)
		return def, nil
	}
	return &cfg, nil
}

func (m *MongoDB) SavePanelConfig(ctx context.Context, cfg *PanelConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	_, err := m.db.Collection("panel_config").UpdateOne(ctx,
		bson.M{"_id": 1},
		bson.M{"$set": cfg},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// ----------------- VK Creator Ops -----------------

type MongoVKCallDoc struct {
	CallID    string `bson:"_id" json:"call_id"`
	Password  string `bson:"password" json:"password"`
	JoinLink  string `bson:"join_link" json:"join_link"`
	VkHash    string `bson:"vk_hash" json:"vk_hash"`
	StartedAt int64  `bson:"started_at" json:"started_at"`
	Finishing bool   `bson:"finishing" json:"finishing"`
}

type MongoVKCookiesDoc struct {
	ID      int    `bson:"_id" json:"id"`
	Raw     string `bson:"raw" json:"raw"`
	SavedAt int64  `bson:"saved_at" json:"saved_at"`
}

func (m *MongoDB) ListVKCalls(ctx context.Context) ([]VKCall, error) {
	coll := m.db.Collection("vk_calls")
	opts := options.Find().SetSort(bson.M{"started_at": -1})
	cur, err := coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var list []VKCall
	for cur.Next(ctx) {
		var doc MongoVKCallDoc
		if err := cur.Decode(&doc); err == nil {
			list = append(list, VKCall{
				CallID:    doc.CallID,
				Password:  doc.Password,
				JoinLink:  doc.JoinLink,
				VkHash:    doc.VkHash,
				StartedAt: doc.StartedAt,
				Finishing: doc.Finishing,
			})
		}
	}
	return list, nil
}

func (m *MongoDB) SaveVKCall(ctx context.Context, c VKCall) error {
	if !ValidVKCallID(c.CallID) {
		return fmt.Errorf("некорректный call_id %q", c.CallID)
	}
	doc := MongoVKCallDoc{
		CallID:    c.CallID,
		Password:  c.Password,
		JoinLink:  c.JoinLink,
		VkHash:    c.VkHash,
		StartedAt: c.StartedAt,
		Finishing: c.Finishing,
	}
	_, err := m.db.Collection("vk_calls").UpdateOne(ctx,
		bson.M{"_id": c.CallID},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (m *MongoDB) DeleteVKCall(ctx context.Context, callID string) error {
	_, err := m.db.Collection("vk_calls").DeleteOne(ctx, bson.M{"_id": callID})
	return err
}

func (m *MongoDB) LoadVKCookies(ctx context.Context) (raw string, savedAt int64, err error) {
	var doc MongoVKCookiesDoc
	err = m.db.Collection("vk_cookies").FindOne(ctx, bson.M{"_id": 1}).Decode(&doc)
	if err != nil {
		return "", 0, err
	}
	return doc.Raw, doc.SavedAt, nil
}

func (m *MongoDB) SaveVKCookies(ctx context.Context, raw string) error {
	doc := MongoVKCookiesDoc{
		ID:      1,
		Raw:     raw,
		SavedAt: time.Now().Unix(),
	}
	_, err := m.db.Collection("vk_cookies").UpdateOne(ctx,
		bson.M{"_id": 1},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (m *MongoDB) ClearVKCookies(ctx context.Context) error {
	_, err := m.db.Collection("vk_cookies").DeleteOne(ctx, bson.M{"_id": 1})
	return err
}

// MigrateFromSQLite импортирует существующую базу SQLite в MongoDB, если коллекция пользователей пуста.
func (m *MongoDB) MigrateFromSQLite(sqlitePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := m.db.Collection("users").CountDocuments(ctx, bson.M{})
	if err != nil || count > 0 {
		return nil // MongoDB уже заполнена или недоступна
	}

	if sqlitePath == "" {
		sqlitePath = DefaultPath
	}
	if _, err := os.Stat(sqlitePath); err != nil {
		return nil // Файл SQLite не существует
	}

	sqlDB, err := Open(sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite for migration: %w", err)
	}
	defer sqlDB.Close()

	store, err := LoadStore(sqlDB)
	if err == nil && store != nil {
		_ = m.SaveStore(ctx, store, SaveOptions{PreserveSubIDs: true})
		log.Printf("[MONGO] Успешно мигрировано %d пользователей из SQLite (%s)", len(store.Users), sqlitePath)
	}

	if in, err := LoadInbound(sqlDB); err == nil && in != nil {
		_ = m.SaveInbound(ctx, in)
	}

	if cfg, err := LoadPanelConfig(sqlDB); err == nil && cfg != nil {
		_ = m.SavePanelConfig(ctx, cfg)
	}

	return nil
}

