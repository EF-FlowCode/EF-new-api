// usage-refund previews and executes administrator-approved balance returns.
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type batch struct {
	ID     string `json:"id"`
	Quota  int64  `json:"quota"`
	Count  int    `json:"count"`
	Status string `json:"status"`
}

type savedPreview struct {
	Server  string          `json:"server"`
	AdminID string          `json:"admin_id"`
	Batch   json.RawMessage `json:"batch"`
}

func csvSelection(reader io.Reader, column, kind string) (map[string]any, error) {
	r := csv.NewReader(reader)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	index := -1
	for i, name := range header {
		if strings.TrimPrefix(strings.TrimSpace(name), "\ufeff") == column {
			if index >= 0 {
				return nil, errors.New("duplicate CSV column")
			}
			index = i
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("CSV column %q not found", column)
	}
	ids := []int64{}
	requests := []string{}
	seen := map[string]bool{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		value := strings.TrimSpace(row[index])
		if value == "" {
			return nil, errors.New("empty identifier in CSV")
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		if kind == "log" {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid log ID %q", value)
			}
			ids = append(ids, id)
		} else {
			requests = append(requests, value)
		}
		if len(seen) > 5000 {
			return nil, errors.New("CSV exceeds 5000 identifiers")
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("CSV has no identifiers")
	}
	switch kind {
	case "log":
		return map[string]any{"log_ids": ids}, nil
	case "request":
		return map[string]any{"request_ids": requests}, nil
	case "upstream":
		return map[string]any{"upstream_request_ids": requests}, nil
	default:
		return nil, errors.New("kind must be log, request, or upstream")
	}
}

func apiRequest(client *http.Client, server, admin, token, method, path string, body any) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, server+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(token, "Bearer "))
	// Scoped nap_ tokens identify their owner; the legacy token needs the user id.
	if admin != "" {
		req.Header.Set("New-Api-User", admin)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("HTTP %d: invalid API response", resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 || !result.Success {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, result.Message)
	}
	return result.Data, nil
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: usage-refund preview|execute|show|list [flags]; credentials: NEW_API_ADMIN_TOKEN (nap_ access token with usage_refund:read/write; legacy tokens also need NEW_API_ADMIN_ID)")
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	server := flags.String("server", os.Getenv("NEW_API_URL"), "new-api base URL")
	admin := flags.String("admin", os.Getenv("NEW_API_ADMIN_ID"), "administrator ID")
	csvFile := flags.String("csv", "", "CSV identifier file")
	column := flags.String("column", "log_id", "CSV identifier column")
	kind := flags.String("kind", "log", "log, request, or upstream")
	selectionFile := flags.String("selection", "", "JSON selection file (alternative to CSV)")
	reason := flags.String("reason", "", "reason for balance return")
	includeGone := flags.Bool("include-client-gone", false, "include client disconnects")
	file := flags.String("file", "", "preview output file, or saved preview for execute")
	confirm := flags.String("confirm", "", "exact batch ID to approve execution")
	id := flags.String("id", "", "batch ID for show")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	*server = strings.TrimRight(*server, "/")
	parsed, err := url.Parse(*server)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("invalid server URL")
	}
	token := strings.TrimSpace(os.Getenv("NEW_API_ADMIN_TOKEN"))
	scoped := strings.HasPrefix(strings.TrimPrefix(token, "Bearer "), "nap_")
	if *admin != "" {
		if adminID, err := strconv.Atoi(*admin); err != nil || adminID <= 0 {
			return errors.New("NEW_API_ADMIN_ID must be a positive user id")
		}
	}
	if token == "" || (!scoped && *admin == "") {
		return errors.New("set NEW_API_ADMIN_TOKEN to a root nap_ access token with usage_refund:read and usage_refund:write (legacy system access tokens also need NEW_API_ADMIN_ID); not a model API key")
	}
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path string, body any) (json.RawMessage, error) {
		return apiRequest(client, *server, *admin, token, method, "/api/log/refunds"+path, body)
	}
	var result json.RawMessage
	switch command {
	case "preview":
		if *file == "" || (*csvFile == "") == (*selectionFile == "") {
			return errors.New("preview requires --file and exactly one of --csv or --selection")
		}
		var selection map[string]any
		if *csvFile != "" {
			f, err := os.Open(*csvFile)
			if err != nil {
				return err
			}
			selection, err = csvSelection(f, *column, *kind)
			f.Close()
			if err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(*selectionFile)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(data, &selection); err != nil {
				return err
			}
		}
		if selection == nil {
			return errors.New("selection must be a JSON object")
		}
		if *reason != "" {
			selection["reason"] = *reason
		}
		if *includeGone {
			selection["include_client_gone"] = true
		}
		result, err = request(http.MethodPost, "/preview", selection)
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(savedPreview{Server: *server, AdminID: *admin, Batch: result}, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(*file, append(data, '\n'), 0600); err != nil {
			return err
		}
	case "execute":
		data, readErr := os.ReadFile(*file)
		if readErr != nil {
			return readErr
		}
		var saved savedPreview
		if err = json.Unmarshal(data, &saved); err != nil {
			return err
		}
		var preview batch
		if err = json.Unmarshal(saved.Batch, &preview); err != nil {
			return err
		}
		if saved.Server != *server || saved.AdminID != *admin || preview.ID == "" || *confirm != preview.ID {
			return errors.New("server/admin must match preview; --confirm must equal the preview batch ID")
		}
		result, err = request(http.MethodPost, "/"+url.PathEscape(preview.ID)+"/execute", map[string]any{"expected_quota": preview.Quota, "expected_count": preview.Count})
		if err != nil {
			status, statusErr := request(http.MethodGet, "/"+url.PathEscape(preview.ID), nil)
			if statusErr == nil {
				fmt.Fprintln(os.Stderr, "Current batch:", string(status))
			}
			return err
		}
	case "show":
		if *id == "" {
			return errors.New("show requires --id")
		}
		result, err = request(http.MethodGet, "/"+url.PathEscape(*id), nil)
	case "list":
		result, err = request(http.MethodGet, "/", nil)
	default:
		return errors.New("unknown command")
	}
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err = json.Indent(&pretty, result, "", "  "); err != nil {
		return err
	}
	fmt.Println(pretty.String())
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
