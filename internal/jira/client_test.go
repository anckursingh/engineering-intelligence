package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchUsesEnhancedJQLRequest(t *testing.T) {
	var method, path string
	var body struct {
		JQL           string   `json:"jql"`
		MaxResults    int      `json:"maxResults"`
		Fields        []string `json:"fields"`
		NextPageToken string   `json:"nextPageToken"`
	}
	var authOK bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode([]map[string]string{{"id": "customfield_10020", "name": "Sprint"}}); err != nil {
				t.Errorf("write field list: %v", err)
			}
			return
		}
		method = r.Method
		path = r.URL.Path
		user, token, ok := r.BasicAuth()
		authOK = ok && user == "user@example.com" && token == "token"
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode search request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(searchPage{NextPageToken: "cursor-3"}); err != nil {
			t.Errorf("write search response: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "user@example.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	page, err := client.search(context.Background(), "project = SCRUM", "cursor-2")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if page == nil {
		t.Fatal("search returned a nil page")
	}

	if method != http.MethodPost || path != "/rest/api/3/search/jql" {
		t.Errorf("request = %s %s, want POST /rest/api/3/search/jql", method, path)
	}
	if body.JQL != "project = SCRUM" || body.MaxResults != 100 || body.NextPageToken != "cursor-2" {
		t.Errorf("request body = %+v, want JQL and 100 results", body)
	}
	if len(body.Fields) != 11 || body.Fields[0] != "key" || body.Fields[7] != "assignee" || body.Fields[8] != "parent" || body.Fields[9] != "project" || body.Fields[10] != "customfield_10020" {
		t.Errorf("fields = %v, want project and the discovered sprint field", body.Fields)
	}
	if !authOK {
		t.Error("request did not use the configured basic-auth credentials")
	}
	if page.NextPageToken != "cursor-3" || page.IsLast {
		t.Errorf("page = %+v, want cursor-3 and isLast=false", page)
	}
}
