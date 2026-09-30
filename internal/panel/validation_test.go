package panel

import "testing"

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "lowercases", input: "Example.COM.", want: "example.com"},
		{name: "subdomain", input: "app.example.com", want: "app.example.com"},
		{name: "requires dot", input: "localhost", wantErr: true},
		{name: "rejects shell characters", input: "example.com;rm", wantErr: true},
		{name: "rejects leading hyphen", input: "-bad.example.com", wantErr: true},
		{name: "rejects underscore", input: "bad_name.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeDomain(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeUpstream(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "loopback", input: "http://127.0.0.1:3000", want: "http://127.0.0.1:3000"},
		{name: "localhost", input: "http://localhost:8080/", want: "http://localhost:8080"},
		{name: "rejects https", input: "https://127.0.0.1:3000", wantErr: true},
		{name: "rejects remote host", input: "http://example.com:3000", wantErr: true},
		{name: "requires port", input: "http://127.0.0.1", wantErr: true},
		{name: "rejects path", input: "http://127.0.0.1:3000/app", wantErr: true},
		{name: "rejects query", input: "http://127.0.0.1:3000?x=1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeUpstream(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeEnvName(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "db_host", want: "DB_HOST"},
		{input: "APP_ENV", want: "APP_ENV"},
		{input: "1BAD", wantErr: true},
		{input: "BAD-NAME", wantErr: true},
	}
	for _, tt := range tests {
		got, err := NormalizeEnvName(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("NormalizeEnvName(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Fatalf("NormalizeEnvName(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
		}
	}
}

func TestNormalizeEnvValue(t *testing.T) {
	if got, err := NormalizeEnvValue("pa$$word!@#"); err != nil || got != "pa$$word!@#" {
		t.Fatalf("NormalizeEnvValue password = %q, %v", got, err)
	}
	if got, err := NormalizeEnvValue("line1\nline2"); err != nil || got != "line1\nline2" {
		t.Fatalf("NormalizeEnvValue multiline = %q, %v", got, err)
	}
	if got, err := NormalizeEnvValue("line1\r\nline2\rline3"); err != nil || got != "line1\nline2\nline3" {
		t.Fatalf("NormalizeEnvValue line endings = %q, %v", got, err)
	}
	if _, err := NormalizeEnvValue("bad\x00value"); err == nil {
		t.Fatal("expected NUL env value to be rejected")
	}
}

func TestNormalizeCronScheduleAndCommand(t *testing.T) {
	if got, err := NormalizeCronSchedule("  */5   * * * * "); err != nil || got != "*/5 * * * *" {
		t.Fatalf("NormalizeCronSchedule = %q, %v", got, err)
	}
	if _, err := NormalizeCronSchedule("@daily"); err == nil {
		t.Fatal("expected cron macro to be rejected")
	}
	if _, err := NormalizeCronSchedule("* * * * * root reboot"); err == nil {
		t.Fatal("expected extra cron fields to be rejected")
	}
	if got, err := NormalizeCronCommand("php artisan schedule:run"); err != nil || got != "php artisan schedule:run" {
		t.Fatalf("NormalizeCronCommand = %q, %v", got, err)
	}
	if _, err := NormalizeCronCommand("echo ok\nwhoami"); err == nil {
		t.Fatal("expected multiline command to be rejected")
	}
	if got, err := NormalizeWorkerName("Queue_Worker"); err != nil || got != "queue-worker" {
		t.Fatalf("NormalizeWorkerName = %q, %v", got, err)
	}
	if _, err := NormalizeWorkerName("../bad"); err == nil {
		t.Fatal("expected invalid worker name to be rejected")
	}
}

func TestNormalizeFirewallAndUpdateInputs(t *testing.T) {
	if got, err := NormalizeFirewallPort("22"); err != nil || got != 22 {
		t.Fatalf("NormalizeFirewallPort = %d, %v; want 22", got, err)
	}
	if _, err := NormalizeFirewallPort("0"); err == nil {
		t.Fatal("expected port 0 to be rejected")
	}
	if got, err := NormalizeFirewallProtocol("TCP"); err != nil || got != "tcp" {
		t.Fatalf("NormalizeFirewallProtocol = %q, %v; want tcp", got, err)
	}
	if _, err := NormalizeFirewallProtocol("icmp"); err == nil {
		t.Fatal("expected unsupported firewall protocol to be rejected")
	}
	if got, err := NormalizeReleaseVersion(""); err != nil || got != "latest" {
		t.Fatalf("NormalizeReleaseVersion empty = %q, %v; want latest", got, err)
	}
	if got, err := NormalizeReleaseVersion("v0.1.6"); err != nil || got != "v0.1.6" {
		t.Fatalf("NormalizeReleaseVersion tag = %q, %v; want v0.1.6", got, err)
	}
	if _, err := NormalizeReleaseVersion("main;rm"); err == nil {
		t.Fatal("expected unsafe release version to be rejected")
	}
	if got, err := NormalizeRepoURL("https://github.com/Gugun09/mini-panel.git"); err != nil || got != "https://github.com/Gugun09/mini-panel" {
		t.Fatalf("NormalizeRepoURL = %q, %v", got, err)
	}
	if _, err := NormalizeRepoURL("https://example.com/Gugun09/mini-panel"); err == nil {
		t.Fatal("expected non-GitHub repo URL to be rejected")
	}
}
