package vertex

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildGoogleModelURLUsesTheLocationHost(t *testing.T) {
	tests := []struct {
		region    string
		projectID string
		want      string
	}{
		{"global", "my-project", "https://aiplatform.googleapis.com/v1/projects/my-project/locations/global/publishers/google/models/gemini-4-argon:generateContent"},
		{"us-central1", "my-project", "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models/gemini-4-argon:generateContent"},
		{"us", "my-project", "https://aiplatform.us.rep.googleapis.com/v1/projects/my-project/locations/us/publishers/google/models/gemini-4-argon:generateContent"},
		{"eu", "my-project", "https://aiplatform.eu.rep.googleapis.com/v1/projects/my-project/locations/eu/publishers/google/models/gemini-4-argon:generateContent"},
		{"eu", "", "https://aiplatform.eu.rep.googleapis.com/v1/publishers/google/models/gemini-4-argon:generateContent"},
		{"", "", "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-4-argon:generateContent"},
	}
	for _, tt := range tests {
		t.Run(tt.region+"/"+tt.projectID, func(t *testing.T) {
			got := BuildGoogleModelURL("", DefaultAPIVersion, tt.projectID, tt.region, "gemini-4-argon", "generateContent")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildAPIBaseURLKeepsTheCustomHostForMultiRegions(t *testing.T) {
	got := BuildAPIBaseURL("https://vertex.example.com/", DefaultAPIVersion, "my-project", "us")
	assert.Equal(t, "https://vertex.example.com/v1/projects/my-project/locations/us", got)
}
