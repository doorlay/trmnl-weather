# trmnl-weather

A [TRMNL](https://usetrmnl.com) plugin that displays an 8-hour weather forecast scraped from AccuWeather. Uses the webhook (push) model — a cron job scrapes the forecast and pushes it to your TRMNL device.

![half_horizontal layout](https://usetrmnl.com/images/plugins/trmnl--render.svg)

## How it works

1. Headless Chrome (via chromedp) loads the AccuWeather hourly forecast page
2. goquery parses the rendered HTML for time, temperature, forecast, and precipitation
3. Forecast text is mapped to weather icons (sunny, cloudy, rain, snow, etc.)
4. Data is POSTed to TRMNL's webhook API as merge variables
5. TRMNL renders the Liquid template on your device

## Prerequisites

- Go 1.24+
- Chrome or Chromium installed locally
- A [TRMNL](https://usetrmnl.com) device

## Setup

### 1. Create a Private Plugin in TRMNL

1. Go to your [TRMNL dashboard](https://usetrmnl.com)
2. Create a new Private Plugin
3. Set the plugin strategy to Webhook / Push
4. Paste the contents of `templates/half_horizontal.liquid` into the **Half Horizontal** template field
5. Save and copy the **Plugin UUID**

### 2. Configure

```bash
cp .env.example .env
```

Edit `.env` and set your `TRMNL_PLUGIN_UUID`. Optionally change `ACCUWEATHER_URL` to a different location's hourly forecast page.

### 3. Build and test

```bash
go build -o webserver .
source .env && export TRMNL_PLUGIN_UUID ACCUWEATHER_URL
./webserver
```

You should see a success message like `Pushed 8 hours of weather data to TRMNL (status 200)`.

### 4. Schedule with cron

```bash
./scripts/setup-cron.sh       # every 15 minutes (default)
./scripts/setup-cron.sh 30    # every 30 minutes
```

Logs are written to `/tmp/trmnl-weather.log`.

## Customization

To change the location, find your city's hourly forecast page on [accuweather.com](https://www.accuweather.com) and set `ACCUWEATHER_URL` in `.env`.

## Template

The `half_horizontal` template displays 8 hourly columns, each showing:

- Time
- Weather icon (sunny, partly cloudy, mostly cloudy, cloudy, rain, thunderstorm, snow, sleet, fog, wind)
- Temperature
- Precipitation chance
