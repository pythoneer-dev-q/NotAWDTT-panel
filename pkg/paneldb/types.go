package paneldb

const DefaultPath = "/etc/wdtt/panel.db"

const (
	DefaultMaxDevices = 1
	MaxDevicesLimit   = 20
	MainUserComment   = "ADMIN" // comment for main_password row in wdtt_users
)

// Store — wdtt_users + wdtt_devices + wdtt_global (ядро VPN-данных).
// Настройки панели — panel_config (panel_config.go), xray — xray.go.
type Store struct {
	MainPassword string
	AdminID      string
	BotToken     string
	Users        map[string]*User
	Devices      map[string]*Device
}

type User struct {
	DeviceID      string
	DeviceIDs     []string
	MaxDevices    int
	ExpiresAt     int64
	DownBytes     int64
	UpBytes       int64
	TotalBytes    int64
	MaxDownMBps   float64
	MaxUpMBps     float64
	IsDeactivated bool
	Comment       string
	Ports         string
	VkHash        string
	WbRoom        string
	SubID         string
	LastSeenAt    int64
	NodeOnlineAt  int64
}

type Device struct {
	DeviceID string
	IP       string
	PrivKey  string
	PubKey   string
}

// NodeUserPresence is activity, traffic and device bindings reported by a VPS node.
type NodeUserPresence struct {
	Password   string   `json:"password"`
	DeviceIDs  []string `json:"device_ids"`
	LastSeenAt int64    `json:"last_seen_at"`
	Online     bool     `json:"online"`
	UpBytes    int64    `json:"up_bytes"`
	DownBytes  int64    `json:"down_bytes"`
}

// SaveOptions — поведение SaveStore.
type SaveOptions struct {
	// PreserveSubIDs: если SubID пуст у пользователя — оставить из БД (сервер).
	PreserveSubIDs bool
}

func NewStore() *Store {
	return &Store{
		Users:   make(map[string]*User),
		Devices: make(map[string]*Device),
	}
}
