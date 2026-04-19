// internal/mfapi/client.go
package mfapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const baseURL = "https://api.mfapi.in"

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// --- Raw API response shapes ---

type schemeListItem struct {
	SchemeCode int    `json:"schemeCode"`
	SchemeName string `json:"schemeName"`
}

type schemeDetailResponse struct {
	Meta struct {
		FundHouse      string `json:"fund_house"`
		SchemeCategory string `json:"scheme_category"`
		SchemeCode     int    `json:"scheme_code"`
		SchemeName     string `json:"scheme_name"`
		SchemeType     string `json:"scheme_type"`
	} `json:"meta"`
	Data []struct {
		Date  string `json:"date"` // "02-01-2006" format from API
		Nav   string `json:"nav"`
	} `json:"data"`
	Status string `json:"status"`
}

// --- Public types ---

type SchemeInfo struct {
	Code      string
	Name      string
	FundHouse string
	Category  string
}

type NAVEntry struct {
	Date  time.Time
	Value float64
}

type SchemeDetail struct {
	Info    SchemeInfo
	History []NAVEntry
}

// FetchAllSchemes returns the full list of scheme codes + names from mfapi.
// This is a single call — use it for scheme discovery.
func (c *Client) FetchAllSchemes(ctx context.Context) ([]SchemeInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/mf", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching scheme list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scheme list returned %d", resp.StatusCode)
	}

	var items []schemeListItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decoding scheme list: %w", err)
	}

	schemes := make([]SchemeInfo, 0, len(items))
	for _, item := range items {
		schemes = append(schemes, SchemeInfo{
			Code: strconv.Itoa(item.SchemeCode),
			Name: item.SchemeName,
		})
	}
	return schemes, nil
}

// FetchSchemeDetail returns full metadata + complete NAV history for one scheme.
func (c *Client) FetchSchemeDetail(ctx context.Context, schemeCode string) (*SchemeDetail, error) {
	url := fmt.Sprintf("%s/mf/%s", baseURL, schemeCode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching scheme %s: %w", schemeCode, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scheme %s returned %d", schemeCode, resp.StatusCode)
	}

	var raw schemeDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding scheme %s: %w", schemeCode, err)
	}

	detail := &SchemeDetail{
		Info: SchemeInfo{
			Code:      schemeCode,
			Name:      raw.Meta.SchemeName,
			FundHouse: raw.Meta.FundHouse,
			Category:  raw.Meta.SchemeCategory,
		},
	}

	for _, d := range raw.Data {
		// mfapi returns dates as "02-01-2006" (DD-MM-YYYY)
		t, err := time.Parse("02-01-2006", d.Date)
		if err != nil {
			continue // skip malformed dates
		}
		val, err := strconv.ParseFloat(d.Nav, 64)
		if err != nil {
			continue // skip malformed NAV values
		}
		detail.History = append(detail.History, NAVEntry{Date: t, Value: val})
	}

	return detail, nil
}