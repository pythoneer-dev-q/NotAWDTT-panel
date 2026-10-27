package server

import (
	"fmt"
	"testing"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

func TestMongoInboundKeepsWGEnabledForDTLSGetconf(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		startup := startupSettingsFromInbound(&paneldb.Inbound{Enable: true, WgEnable: enabled, RawEnable: true, WgPort: 56001})
		if startup.WgEnable != enabled || startup.RuntimeSettings.WgEnable != enabled {
			t.Fatalf("wg_enable=%v was lost while loading MongoDB inbound", enabled)
		}
	}
}

func TestWGModeUsesFlagWithoutPanelDatabase(t *testing.T) {
	initServerFlags()
	previous := *flagWgEnable
	defer func() { *flagWgEnable = previous }()

	*flagWgEnable = true
	if !baseServerConfigFromFlags().WgEnable {
		t.Fatal("standalone node disabled WireGuard despite -wg-enable=true")
	}
	*flagWgEnable = false
	if baseServerConfigFromFlags().WgEnable {
		t.Fatal("explicit -wg-enable=false was ignored")
	}
}

func TestWGAllocatorContinuesBeyondLegacySubnets(t *testing.T) {
	dbMutex.Lock()
	previous := db
	db = &Database{Devices: make(map[string]*ClientDevice)}
	defer func() {
		db = previous
		dbMutex.Unlock()
	}()

	if ip := getNextIP(); ip != "10.66.66.2" {
		t.Fatalf("first address changed: %s", ip)
	}
	for subnet := 66; subnet <= 75; subnet++ {
		for host := 2; host <= 254; host++ {
			ip := fmt.Sprintf("10.66.%d.%d", subnet, host)
			db.Devices[ip] = &ClientDevice{IP: ip}
		}
	}
	if ip := getNextIP(); ip != "10.66.0.2" {
		t.Fatalf("allocator stopped after old ten subnets: %s", ip)
	}
	if wgServerCIDR != "10.66.66.1/16" || wgSubnetCIDR != "10.66.0.0/16" || maxUsersSubnetLimit != 64768 {
		t.Fatal("WG address range and capacity disagree")
	}
	if ip := wgIPForIndex(maxUsersSubnetLimit - 1); ip != "10.66.255.254" {
		t.Fatalf("last address outside the /16 pool: %s", ip)
	}
}

func TestWGAllocatorDoesNotReuseAnAssignedAddress(t *testing.T) {
	dbMutex.Lock()
	previous := db
	db = &Database{Devices: make(map[string]*ClientDevice)}
	defer func() {
		db = previous
		dbMutex.Unlock()
	}()

	first := getNextIP()
	db.Devices["a"] = &ClientDevice{IP: first}
	second := getNextIP()
	if first == second || second != "10.66.66.3" {
		t.Fatalf("allocated duplicate or unexpected IP: %s, %s", first, second)
	}
}
