package sysproxy

import "testing"

func TestServerString(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"127.0.0.1", 9190, "127.0.0.1:9190"},
		{"::1", 7890, "[::1]:7890"},
		{"proxy.local", 8080, "proxy.local:8080"},
	}
	for _, tt := range tests {
		if got := serverString(tt.host, tt.port); got != tt.want {
			t.Errorf("serverString(%q, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}
}

func TestEnableRejectsInvalidPort(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		if err := Enable("127.0.0.1", port); err == nil {
			t.Errorf("Enable with port %d: expected error, got nil", port)
		}
	}
}
