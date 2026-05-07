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
	Time        string
	Temperature string
	RealFeel    string
	Forecast    string
	Icon        string
}

type DailyData struct {
	CurrentTemp     string
	CurrentRealFeel string
	CurrentForecast string
	HighTemp        string
	LowTemp         string
	RainPercent     string
}

type DayOverview struct {
	Date            string `json:"date"`
	CurrentTemp     string `json:"currentTemp"`
	CurrentRealFeel string `json:"currentRealFeel"`
	CurrentIcon     string `json:"currentIcon"`
	HighTemp        string `json:"highTemp"`
	LowTemp         string `json:"lowTemp"`
	RainPercent     string `json:"rainPercent"`
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

	hourlyURL := os.Getenv("ACCUWEATHER_URL")
	if hourlyURL == "" {
		hourlyURL = "https://www.accuweather.com/en/us/capitol-hill/98102/hourly-weather-forecast/2254014"
	}
	dailyURL := strings.Replace(hourlyURL, "/hourly-weather-forecast/", "/weather-forecast/", 1)

	lat := os.Getenv("LATITUDE")
	lon := os.Getenv("LONGITUDE")
	sunrise, sunset := getSunTimes(lat, lon)

	dailyHTML := scrapeDailyPage(dailyURL)
	daily := parseDailyHTML(dailyHTML)

	hourlyHTML := scrapeWeatherPage(hourlyURL)
	hours := parseHourlyHTML(hourlyHTML, sunrise, sunset)

	overview := combineOverview(daily, hours)
	overview.Date = time.Now().Format("Mon, Jan 2")

	location := os.Getenv("LOCATION_NAME")
	if location == "" {
		location = "Weather"
	}

	pushToTRMNL(pluginUUID, location, overview)
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
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	if err != nil {
		log.Fatalf("Chromedp error: %v", err)
	}

	return html
}

// scrapeDailyPage loads AccuWeather's daily forecast page and returns the rendered HTML.
// No clicks — clicking the daily-list-item replaces the static structure with a panel view.
func scrapeDailyPage(url string) string {
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
		chromedp.WaitVisible(`.cur-con-weather-card`, chromedp.ByQuery),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	if err != nil {
		log.Printf("Warning: daily page scrape failed: %v", err)
		return ""
	}

	return html
}

func parseDailyHTML(html string) DailyData {
	var d DailyData
	if html == "" {
		return d
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return d
	}

	card := doc.Find(".cur-con-weather-card").First()
	d.CurrentTemp = strings.TrimSpace(card.Find(".temp").First().Text())
	d.CurrentForecast = strings.TrimSpace(card.Find(".phrase").First().Text())

	rfText := strings.Join(strings.Fields(card.Find(".real-feel").First().Text()), " ")
	rfText = strings.TrimPrefix(rfText, "RealFeel®")
	rfText = strings.TrimSpace(rfText)
	if idx := strings.Index(rfText, "°"); idx >= 0 {
		d.CurrentRealFeel = strings.TrimSpace(rfText[:idx+len("°")])
	}

	first := doc.Find(".daily-list-item").First()
	hiText := strings.TrimSpace(first.Find(".temp-hi").First().Text())
	loText := strings.TrimSpace(first.Find(".temp-lo").First().Text())
	switch {
	case strings.Contains(loText, "°"):
		d.HighTemp = hiText
		d.LowTemp = loText
	case strings.EqualFold(loText, "Lo"):
		d.LowTemp = hiText
	case strings.EqualFold(loText, "Hi"):
		d.HighTemp = hiText
	default:
		d.HighTemp = hiText
	}

	d.RainPercent = strings.TrimSpace(first.Find(".precip").First().Text())
	return d
}

// formatTime normalizes AccuWeather time strings to "3 PM" style.
func formatTime(t string) string {
	if strings.Contains(t, "AM") || strings.Contains(t, "PM") {
		return t
	}
	parsed, err := time.Parse("15:04", strings.TrimSpace(t))
	if err != nil {
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
func getSunTimes(lat, lon string) (sunrise, sunset int) {
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

		timeStr := formatTime(t)
		hour := parseHour(timeStr)
		isNight := hour < sunrise || hour >= sunset

		results = append(results, HourData{
			Time:        timeStr,
			Temperature: temp,
			RealFeel:    realFeel,
			Forecast:    forecast,
			Icon:        mapForecastToIcon(forecast, isNight),
		})
	})

	return results
}

// combineOverview computes the day's overview. Feels-like (current/high/low) is
// aggregated from hourly hours of the remaining day. Rain percent comes from the
// daily forecast page; rain timing and inches come from hourly. Hourly is
// filtered to today (stops at midnight crossing).
func combineOverview(daily DailyData, hours []HourData) DayOverview {
	overview := DayOverview{
		CurrentTemp: daily.CurrentTemp,
		RainPercent: daily.RainPercent,
	}
	if overview.RainPercent == "" {
		overview.RainPercent = "0%"
	}

	var today []HourData
	if len(hours) > 0 {
		today = append(today, hours[0])
		prev := parseHour(hours[0].Time)
		for _, h := range hours[1:] {
			hr := parseHour(h.Time)
			if hr < prev {
				break
			}
			today = append(today, h)
			prev = hr
		}
	}

	if len(today) > 0 {
		overview.CurrentIcon = today[0].Icon
		overview.CurrentRealFeel = today[0].RealFeel
		if overview.CurrentTemp == "" {
			overview.CurrentTemp = today[0].Temperature
		}
	} else {
		overview.CurrentIcon = mapForecastToIcon(daily.CurrentForecast, false)
	}

	highVal, lowVal := -999, 999
	for _, h := range today {
		v, ok := parseTemp(h.RealFeel)
		if !ok {
			continue
		}
		if v > highVal {
			highVal = v
			overview.HighTemp = strings.TrimSpace(h.RealFeel)
		}
		if v < lowVal {
			lowVal = v
			overview.LowTemp = strings.TrimSpace(h.RealFeel)
		}
	}

	return overview
}

func parseTemp(s string) (int, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "°", "")
	s = strings.ReplaceAll(s, "F", "")
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// mapForecastToIcon maps AccuWeather forecast phrases to icon keywords.
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

func pushToTRMNL(pluginUUID string, location string, overview DayOverview) {
	payload := map[string]any{
		"merge_variables": map[string]any{
			"location":        location,
			"date":            overview.Date,
			"currentTemp":     overview.CurrentTemp,
			"currentRealFeel": overview.CurrentRealFeel,
			"currentIcon":     overview.CurrentIcon,
			"highTemp":        overview.HighTemp,
			"lowTemp":         overview.LowTemp,
			"rainPercent":     overview.RainPercent,
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

	fmt.Printf("Pushed daily overview to TRMNL (status %d)\n", resp.StatusCode)
}
