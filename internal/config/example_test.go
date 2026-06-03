package config_test

import (
	"os"
	"testing"
	"gopkg.in/yaml.v3"
	"github.com/abbasnaqdi/ng-ray/internal/config"
)

func TestExampleConfig(t *testing.T) {
	b, err := os.ReadFile("../../config.example.yml")
	if err != nil {
		t.Skip("no config.example.yml")
	}
	c := config.Default()
	if err := yaml.Unmarshal(b, c); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	t.Logf("xray-reality=%d xhttp=%d shoes=%d singbox=%d hy2=%v tuic=%v altports=%v routes=%v",
		len(c.Cores.Xray.Reality), len(c.Cores.Xray.XHTTP),
		len(c.Cores.Shoes.Reality), len(c.Cores.Singbox.Reality),
		c.Cores.Hysteria2.Enabled,
		c.Cores.Singbox.TUIC != nil && c.Cores.Singbox.TUIC.Enabled,
		c.Cores.Hysteria2.AltPorts,
		c.RouteSNIs())
}
