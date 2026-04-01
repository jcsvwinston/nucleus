package tasks

import "testing"

func TestRedisClientOptFromURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		addr      string
		password  string
		db        int
		shouldErr bool
	}{
		{
			name:  "basic url",
			input: "redis://localhost",
			addr:  "localhost:6379",
			db:    0,
		},
		{
			name:     "with password and db",
			input:    "redis://:secret@redis.internal:6380/3",
			addr:     "redis.internal:6380",
			password: "secret",
			db:       3,
		},
		{
			name:      "invalid scheme",
			input:     "http://localhost:6379/0",
			shouldErr: true,
		},
		{
			name:      "invalid db",
			input:     "redis://localhost/not-a-db",
			shouldErr: true,
		},
		{
			name:      "empty",
			input:     "",
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opt, err := redisClientOptFromURL(tc.input)
			if tc.shouldErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if opt.Addr != tc.addr {
				t.Fatalf("expected addr %q, got %q", tc.addr, opt.Addr)
			}
			if opt.Password != tc.password {
				t.Fatalf("expected password %q, got %q", tc.password, opt.Password)
			}
			if opt.DB != tc.db {
				t.Fatalf("expected db %d, got %d", tc.db, opt.DB)
			}
		})
	}
}

func TestNewJSONTask(t *testing.T) {
	task, err := NewJSONTask("emails.send_welcome", map[string]string{"email": "alice@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task == nil {
		t.Fatal("expected task")
	}
	if task.Type() != "emails.send_welcome" {
		t.Fatalf("unexpected task type: %s", task.Type())
	}
	if string(task.Payload()) != `{"email":"alice@example.com"}` {
		t.Fatalf("unexpected payload: %s", string(task.Payload()))
	}
}

func TestNewJSONTask_RequiresType(t *testing.T) {
	_, err := NewJSONTask("", map[string]string{"x": "1"})
	if err == nil {
		t.Fatal("expected error for empty task type")
	}
}
