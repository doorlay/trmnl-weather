package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"github.com/joho/godotenv"
)

const trmnlAPI = "https://usetrmnl.com/api/custom_plugins"

type HourData struct {
	Time        string `json:"time"`
	Temperature string `json:"temperature"`
	RealFeel    string `json:"realFeel"`
	Forecast    string `json:"forecast"`
	Icon        string `json:"icon"`
	Precip      string `json:"precip"`
	Rain        string `json:"rain"`
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	pluginUUID := os.Getenv("TRMNL_PLUGIN_UUID")
	if pluginUUID == "" {
		log.Fatal("TRMNL_PLUGIN_UUID environment variable is required")
	}

	url := os.Getenv("ACCUWEATHER_URL")
	if url == "" {
		url = "https://www.accuweather.com/en/us/capitol-hill/98102/hourly-weather-forecast/2254014"
	}

	// Fetch sunrise/sunset times for day/night icon selection
	lat := os.Getenv("LATITUDE")
	lon := os.Getenv("LONGITUDE")
	sunrise, sunset := getSunTimes(lat, lon)

	html := scrapeWeatherPage(url)
	hours := parseHourlyHTML(html, sunrise, sunset)

	// If today doesn't have 8 hours, scrape tomorrow too
	if len(hours) < 8 {
		tomorrowURL := url + "?day=2"
		tomorrowHTML := scrapeWeatherPage(tomorrowURL)
		hours = append(hours, parseHourlyHTML(tomorrowHTML, sunrise, sunset)...)
	}

	// Limit to 8 hours
	if len(hours) > 8 {
		hours = hours[:8]
	}

	location := os.Getenv("LOCATION_NAME")
	if location == "" {
		location = "Weather"
	}

	pushToTRMNL(pluginUUID, location, hours)
}

func scrapeWeatherPage(url string) string {
	allocOpts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("start-maximized", false),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var html string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`div.accordion-item.hour`, chromedp.ByQuery),
		// Click all accordion items to expand them and load rain amounts
		chromedp.Evaluate(`document.querySelectorAll('div.accordion-item.hour').forEach(el => el.click())`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	if err != nil {
		log.Fatalf("Chromedp error: %v", err)
	}

	return html
}

// formatTime normalizes AccuWeather time strings to "3 PM" style.
// Handles "15:00" (24h) and passes through "3 PM" as-is.
func formatTime(t string) string {
	// Already in 12h format
	if strings.Contains(t, "AM") || strings.Contains(t, "PM") {
		return t
	}
	// Try parsing 24h format
	parsed, err := time.Parse("15:04", strings.TrimSpace(t))
	if err != nil {
		// Try just the hour
		parsed, err = time.Parse("15", strings.TrimSpace(t))
		if err != nil {
			return t
		}
	}
	h := parsed.Hour()
	suffix := "AM"
	if h >= 12 {
		suffix = "PM"
	}
	if h == 0 {
		h = 12
	} else if h > 12 {
		h -= 12
	}
	return fmt.Sprintf("%d %s", h, suffix)
}

// getSunTimes fetches today's sunrise/sunset hours from sunrise-sunset.org.
// Returns sunrise and sunset as hour integers in local time (e.g. 6, 20).
func getSunTimes(lat, lon string) (sunrise, sunset int) {
	// Defaults if we can't fetch
	sunrise, sunset = 6, 20

	if lat == "" || lon == "" {
		return
	}

	url := fmt.Sprintf("https://api.sunrise-sunset.org/json?lat=%s&lng=%s&formatted=0", lat, lon)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Warning: could not fetch sunrise/sunset: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var result struct {
		Results struct {
			Sunrise string `json:"sunrise"`
			Sunset  string `json:"sunset"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return
	}

	// Parse ISO 8601 times and convert to local hour
	if t, err := time.Parse(time.RFC3339, result.Results.Sunrise); err == nil {
		sunrise = t.Local().Hour()
	}
	if t, err := time.Parse(time.RFC3339, result.Results.Sunset); err == nil {
		sunset = t.Local().Hour()
	}

	return
}

// parseHour extracts the hour (0-23) from a time string like "3 PM" or "15:00".
func parseHour(t string) int {
	t = strings.TrimSpace(t)
	if strings.Contains(t, "AM") || strings.Contains(t, "PM") {
		isPM := strings.Contains(t, "PM")
		numStr := strings.TrimSpace(strings.Replace(strings.Replace(t, "AM", "", 1), "PM", "", 1))
		h, err := strconv.Atoi(numStr)
		if err != nil {
			return -1
		}
		if h == 12 {
			h = 0
		}
		if isPM {
			h += 12
		}
		return h
	}
	// Try 24h
	parts := strings.Split(t, ":")
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return -1
	}
	return h
}

func parseHourlyHTML(html string, sunrise, sunset int) []HourData {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		log.Fatalf("Failed reading HTML: %v", err)
	}

	var results []HourData

	doc.Find("div.accordion-item.hour").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Find(".date div").First().Text())
		temp := strings.TrimSpace(s.Find(".temp").First().Text())
		realFeel := strings.TrimSpace(s.Find(".real-feel__text").First().Text())
		realFeel = strings.TrimPrefix(realFeel, "RealFeel®")
		realFeel = strings.TrimSpace(realFeel)
		forecast := strings.TrimSpace(s.Find(".phrase").First().Text())
		precip := strings.TrimSpace(s.Find(".precip").First().Text())

		// Extract rain amount from expanded panel
		var rain string
		s.Find(".accordion-item-content .panel p").Each(func(j int, p *goquery.Selection) {
			valueText := strings.TrimSpace(p.Find(".value").Text())
			label := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p.Text()), valueText))
			if label == "Rain" || label == "Snow" || label == "Ice" {
				rain = valueText
			}
		})

		timeStr := formatTime(t)
		hour := parseHour(timeStr)
		isNight := hour < sunrise || hour >= sunset

		results = append(results, HourData{
			Time:        timeStr,
			Temperature: temp,
			RealFeel:    realFeel,
			Forecast:    forecast,
			Icon:        mapForecastToIcon(forecast, isNight),
			Precip:      precip,
			Rain:        rain,
		})
	})

	return results
}

// mapForecastToIcon maps AccuWeather forecast phrases to icon keywords
// used by the Liquid template to select the correct SVG.
// isNight is determined by comparing the hour against sunrise/sunset times.
func mapForecastToIcon(forecast string, isNight bool) string {
	f := strings.ToLower(forecast)

	switch {
	case strings.Contains(f, "thunder") || strings.Contains(f, "t-storm"):
		return "thunder"
	case strings.Contains(f, "snow") || strings.Contains(f, "flurr"):
		return "snow"
	case strings.Contains(f, "ice") || strings.Contains(f, "sleet") || strings.Contains(f, "freezing"):
		return "rain"
	case strings.Contains(f, "rain") || strings.Contains(f, "shower") || strings.Contains(f, "drizzle"):
		return "rain"
	case strings.Contains(f, "fog") || strings.Contains(f, "haz") || strings.Contains(f, "mist"):
		return "cloud"
	case strings.Contains(f, "wind"):
		return "cloud"
	case strings.Contains(f, "partly cloudy") || strings.Contains(f, "partly sunny") ||
		strings.Contains(f, "mostly cloudy") || strings.Contains(f, "mostly sunny") ||
		strings.Contains(f, "mostly clear") || strings.Contains(f, "intermittent"):
		if isNight {
			return "cloudy_night"
		}
		return "partly_cloudy"
	case strings.Contains(f, "overcast") || strings.Contains(f, "cloudy"):
		return "cloud"
	case strings.Contains(f, "clear"):
		if isNight {
			return "clear_night"
		}
		return "sunny"
	case strings.Contains(f, "sunny"):
		return "sunny"
	default:
		return "cloud"
	}
}

func pushToTRMNL(pluginUUID string, location string, hours []HourData) {
	payload := map[string]any{
		"merge_variables": map[string]any{
			"location": location,
			"hours":    hours,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}

	url := fmt.Sprintf("%s/%s", trmnlAPI, pluginUUID)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("Failed to push to TRMNL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Fatalf("TRMNL API returned status %d", resp.StatusCode)
	}

	fmt.Printf("Pushed %d hours of weather data to TRMNL (status %d)\n", len(hours), resp.StatusCode)
}
