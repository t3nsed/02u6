package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"
)

const (
	OLLAMA_API_BASE   = "http://localhost:11434"
	LISTEN_ADDR       = ":8080"
	CONTENT_TYPE_JSON = "application/json"
)

type OpenAIChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OllamaRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Stream  bool   `json:"stream"`
	Options struct {
		Temperature float64 `json:"temperature,omitempty"`
		NumPredict  int     `json:"num_predict,omitempty"`
	} `json:"options"`
}

type OllamaResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type ErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func main() {
	handler := corsMiddleware(http.HandlerFunc(handleChatCompletions))
	http.Handle("/v1/chat/completions", handler)
	log.Printf("Starting server on %s", LISTEN_ADDR)
	log.Fatal(http.ListenAndServe(LISTEN_ADDR, nil))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", CONTENT_TYPE_JSON)

	if r.Method != http.MethodPost {
		sendError(w, "Method not allowed", "invalid_request_error", "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}

	var openAIReq OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&openAIReq); err != nil {
		sendError(w, "Invalid request body", "invalid_request_error", "invalid_body", http.StatusBadRequest)
		return
	}

	if len(openAIReq.Messages) == 0 {
		sendError(w, "Messages array is empty", "invalid_request_error", "invalid_messages", http.StatusBadRequest)
		return
	}

	if openAIReq.Model == "" {
		sendError(w, "Model is required", "invalid_request_error", "invalid_model", http.StatusBadRequest)
		return
	}

	prompt := convertMessagesToPrompt(openAIReq.Messages)

	ollamaReq := OllamaRequest{
		Model:  openAIReq.Model,
		Prompt: prompt,
		Stream: openAIReq.Stream,
	}

	if openAIReq.Temperature > 0 {
		ollamaReq.Options.Temperature = openAIReq.Temperature
	}
	if openAIReq.MaxTokens > 0 {
		ollamaReq.Options.NumPredict = openAIReq.MaxTokens
	}

	ollamaResp, err := sendToOllama(ollamaReq)
	if err != nil {
		sendError(w, "Error calling Ollama API: "+err.Error(), "server_error", "internal_error", http.StatusInternalServerError)
		return
	}

	openAIResp := OpenAIChatResponse{
		ID:      "chatcmpl-" + generateRandomString(10),
		Object:  "chat.completion",
		Created: getCurrentUnixTimestamp(),
		Model:   ollamaReq.Model,
		Choices: []Choice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: ollamaResp.Response,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     len(prompt) / 4,              // Rough estimation
			CompletionTokens: len(ollamaResp.Response) / 4, // Rough estimation
			TotalTokens:      (len(prompt) + len(ollamaResp.Response)) / 4,
		},
	}

	json.NewEncoder(w).Encode(openAIResp)
}

func sendToOllama(req OllamaRequest) (*OllamaResponse, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(OLLAMA_API_BASE+"/api/generate", CONTENT_TYPE_JSON, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var ollamaResp OllamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &ollamaResp, nil
}

func convertMessagesToPrompt(messages []ChatMessage) string {
	var prompt string
	for _, msg := range messages {
		prompt += msg.Role + ": " + msg.Content + "\n"
	}
	return prompt
}

func getCurrentUnixTimestamp() int64 {
	return time.Now().Unix()
}

// this literally doesn't matter, but some ppl think it does so we're going to just give them a dumb response
func generateRandomString(n int) string {
	const letters = "greatJobOnThatUselessRegex000"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func sendError(w http.ResponseWriter, message string, errorType string, code string, status int) {
	w.Header().Set("Content-Type", CONTENT_TYPE_JSON)
	w.WriteHeader(status)
	resp := ErrorResponse{}
	resp.Error.Message = message
	resp.Error.Type = errorType
	resp.Error.Code = code
	json.NewEncoder(w).Encode(resp)
}
