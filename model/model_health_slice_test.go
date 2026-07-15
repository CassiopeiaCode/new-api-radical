package model

import "testing"

func TestIsQualifiedSuccessWhisperResponseBytesThreshold(t *testing.T) {
	tests := []struct {
		name          string
		modelName     string
		responseBytes int
		completionTokens int
		want          bool
	}{
		{name: "below threshold", modelName: "whisper-large-v3-turbo", responseBytes: 99, want: false},
		{name: "at threshold", modelName: "whisper-large-v3-turbo", responseBytes: 100, want: true},
		{name: "case insensitive prefix", modelName: "WhIsPeR-large-v3-turbo", responseBytes: 100, want: true},
		{name: "ordinary model keeps original threshold", modelName: "bge-m3", responseBytes: 100, want: false},
		{name: "ordinary model completion threshold", modelName: "bge-m3", completionTokens: 3, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsQualifiedSuccess(tt.modelName, tt.responseBytes, tt.completionTokens, 0)
			if got != tt.want {
				t.Fatalf("IsQualifiedSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelHealthEventNormalizeWhisperErrorStillFails(t *testing.T) {
	event := &ModelHealthEvent{
		ModelName:     "whisper-large-v3-turbo",
		CreatedAt:     1,
		IsError:       true,
		ResponseBytes: 100,
	}
	if err := event.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if event.SuccessIsQualified {
		t.Fatal("expected error event to remain unqualified")
	}
}
