package config

import (
    "os"
    "path/filepath"
    "testing"
)

// A previously installed machine that did not opt in should obtain TCP
// connect RTT support automatically after the v1.0.3 binary upgrade.
func TestMachineTCPProbeV103DefaultAndOptOut(t *testing.T) {
    for _, tc := range []struct{
        name, yamlValue string
        expected bool
    }{
        {"omitted", "", true},
        {"explicit-true", "  tcp_probe: true\n", true},
        {"explicit-false", "  tcp_probe: false\n", false},
    } {
        t.Run(tc.name,func(t *testing.T){
            cfgPath := filepath.Join(t.TempDir(),"config.yml")
            data := "panel:\n  url: https://example.test\nmachine:\n  machine_id: 24\n  token: test-token\nkernel:\n  type: singbox\n"+tc.yamlValue
            if err := os.WriteFile(cfgPath,[]byte(data),0600); err != nil { t.Fatal(err) }
            cfg,err := Load(cfgPath)
            if err != nil { t.Fatal(err) }
            node := cfg.ExpandMachineNode(18,"hysteria")
            if node.Kernel.TCPProbe != tc.expected {
                t.Fatalf("machine tcp_probe=%v want %v",node.Kernel.TCPProbe,tc.expected)
            }
            tuic := cfg.ExpandMachineNode(19,"tuic")
            if tuic.Kernel.TCPProbe != tc.expected {
                t.Fatalf("TUIC machine tcp_probe=%v want %v",tuic.Kernel.TCPProbe,tc.expected)
            }
        })
    }
}

func TestStandaloneNodeTCPProbeRemainsOptIn(t *testing.T) {
    cfg := &Config{Kernel:KernelConfig{}}
    if cfg.Kernel.TCPProbe {
        t.Fatal("standalone and single-node modes must not change default")
    }
}

func TestMachineTCPProbeMultiInstanceInheritance(t *testing.T) {
    for _, tc := range []struct{
        name string
        top,instance string
        expected bool
    }{
        {"inherited-explicit-false","  tcp_probe: false\n","",false},
        {"inherited-explicit-true","  tcp_probe: true\n","",true},
        {"instance-false-overrides-parent-true","  tcp_probe: true\n","      tcp_probe: false\n",false},
        {"omitted-default-enabled","","",true},
    } {
        t.Run(tc.name,func(t *testing.T){
            cfgPath:=filepath.Join(t.TempDir(),"config.yml")
            data:="kernel:\n  type: singbox\n"+tc.top+"instances:\n  - panel:\n      url: https://example.test\n    machine:\n      machine_id: 24\n      token: test-token\n    kernel:\n      type: singbox\n"+tc.instance
            if err:=os.WriteFile(cfgPath,[]byte(data),0600);err!=nil {t.Fatal(err)}
            root,err:=LoadRoot(cfgPath)
            if err!=nil {t.Fatal(err)}
            instances,err:=root.NormalizeInstances()
            if err!=nil {t.Fatal(err)}
            got:=instances[0].ExpandMachineNode(18,"hysteria").Kernel.TCPProbe
            if got!=tc.expected {t.Fatalf("probe=%v, want %v",got,tc.expected)}
        })
    }
}
