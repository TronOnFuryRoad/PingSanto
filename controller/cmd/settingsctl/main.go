package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	baseURL := flag.String("base-url", os.Getenv("CONTROLLER_BASE_URL"), "Controller base URL")
	token := flag.String("token", os.Getenv("CONTROLLER_ADMIN_TOKEN"), "Admin bearer token")
	set := flag.String("set", "", "Set notification toggle (true|false)")
	flag.Parse()

	if *baseURL == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "base-url and token are required")
		os.Exit(1)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	if *set == "" {
		resp, err := doRequest(client, http.MethodGet, *baseURL, *token, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(string(resp))
		return
	}

	value, err := parseBool(*set)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	payload := map[string]any{"notify_on_publish": value}
	body, _ := json.Marshal(payload)
	resp, err := doRequest(client, http.MethodPost, *baseURL, *token, body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}

func doRequest(client *http.Client, method, baseURL, token string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("%s/api/admin/v1/settings/notifications", baseURL), nil)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("controller responded with %s: %s", resp.Status, string(data))
	}
	return data, nil
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s", value)
	}
}
