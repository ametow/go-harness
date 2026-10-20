package main

import "testing"

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		args    []string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults",
			want: Config{Model: "qwen3.5:9b", URL: "http://localhost:11434", System: defaultSystemPrompt, MaxSteps: 10},
		},
		{
			name: "env overrides default",
			env:  map[string]string{"HARNESS_MODEL": "llama3", "HARNESS_MAX_STEPS": "3"},
			want: Config{Model: "llama3", URL: "http://localhost:11434", System: defaultSystemPrompt, MaxSteps: 3},
		},
		{
			name: "flag overrides env",
			env:  map[string]string{"HARNESS_MODEL": "llama3"},
			args: []string{"-model", "qwen3.5:9b", "-system", "Be terse."},
			want: Config{Model: "qwen3.5:9b", URL: "http://localhost:11434", System: "Be terse.", MaxSteps: 10},
		},
		{name: "bad env number", env: map[string]string{"HARNESS_MAX_STEPS": "lots"}, wantErr: true},
		{name: "zero steps", args: []string{"-max-steps", "0"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"HARNESS_MODEL", "HARNESS_URL", "HARNESS_SYSTEM", "HARNESS_MAX_STEPS"} {
				t.Setenv(k, "") // start each case from a clean environment
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got, err := parseConfig(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
