package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChromaClient_Heartbeat(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "/api/v2/heartbeat", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"nanosecond heartbeat": 1712500000000000000}`))
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		require.NoError(t, client.Heartbeat(context.Background()))
	})

	t.Run("server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not ready"))
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		err := client.Heartbeat(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "503")
	})

	t.Run("server_down", func(t *testing.T) {
		client := NewChromaClient("http://127.0.0.1:1")
		err := client.Heartbeat(context.Background())
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChromaUnavailable)
	})
}

func TestChromaClient_GetOrCreateCollection(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			require.Contains(t, r.URL.Path, "/collections")

			var req map[string]interface{}
			json.NewDecoder(r.Body).Decode(&req)
			require.Equal(t, "test_collection", req["name"])
			require.Equal(t, true, req["get_or_create"])

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(chromaCollectionResponse{
				ID:   "col-123",
				Name: "test_collection",
			})
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		id, err := client.GetOrCreateCollection(context.Background(), "test_collection")
		require.NoError(t, err)
		require.Equal(t, "col-123", id)
	})

	t.Run("server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		_, err := client.GetOrCreateCollection(context.Background(), "test")
		require.Error(t, err)
		require.Contains(t, err.Error(), "500")
	})

	t.Run("server_down", func(t *testing.T) {
		client := NewChromaClient("http://127.0.0.1:1")
		_, err := client.GetOrCreateCollection(context.Background(), "test")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChromaUnavailable)
	})
}

func TestChromaClient_AddDocuments(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Contains(t, r.URL.Path, "/add")

			var req chromaAddRequest
			json.NewDecoder(r.Body).Decode(&req)
			require.Equal(t, []string{"doc1", "doc2"}, req.IDs)
			require.Len(t, req.Embeddings, 2)
			require.Len(t, req.Metadatas, 2)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		err := client.AddDocuments(context.Background(), "col-123",
			[]string{"doc1", "doc2"},
			[][]float64{{0.1, 0.2}, {0.3, 0.4}},
			[]map[string]string{{"title": "Doc 1"}, {"title": "Doc 2"}},
		)
		require.NoError(t, err)
	})
}

func TestChromaClient_Query(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Contains(t, r.URL.Path, "/query")

			var req chromaQueryRequest
			json.NewDecoder(r.Body).Decode(&req)
			require.Equal(t, 5, req.NResults)

			resp := chromaQueryResponse{
				IDs:       [][]string{{"doc1", "doc2"}},
				Distances: [][]float64{{0.1, 0.5}},
				Metadatas: [][]map[string]string{
					{{"title": "Doc 1", "note_id": "n1"}, {"title": "Doc 2", "note_id": "n2"}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		results, err := client.Query(context.Background(), "col-123", []float64{0.1, 0.2}, 5)
		require.NoError(t, err)
		require.Len(t, results, 2)
		require.Equal(t, "doc1", results[0].ID)
		require.InDelta(t, 0.1, results[0].Distance, 0.001)
		require.Equal(t, "n1", results[0].Metadata["note_id"])
	})

	t.Run("empty_results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := chromaQueryResponse{
				IDs:       [][]string{{}},
				Distances: [][]float64{{}},
				Metadatas: [][]map[string]string{{}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		results, err := client.Query(context.Background(), "col-123", []float64{0.1}, 5)
		require.NoError(t, err)
		require.Empty(t, results)
	})
}

func TestChromaClient_UpsertDocuments(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Contains(t, r.URL.Path, "/upsert")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		err := client.UpsertDocuments(context.Background(), "col-123",
			[]string{"doc1"},
			[][]float64{{0.1, 0.2}},
			[]map[string]string{{"title": "Doc 1"}},
		)
		require.NoError(t, err)
	})
}

func TestChromaClient_DeleteDocuments(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Contains(t, r.URL.Path, "/delete")

			var req chromaDeleteRequest
			json.NewDecoder(r.Body).Decode(&req)
			require.Equal(t, []string{"doc1", "doc2"}, req.IDs)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewChromaClient(server.URL)
		err := client.DeleteDocuments(context.Background(), "col-123", []string{"doc1", "doc2"})
		require.NoError(t, err)
	})
}

func TestCollectionName(t *testing.T) {
	require.Equal(t, "user_abc123_notes", CollectionName("abc123"))
}
