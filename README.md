# trmnl-weather

A [TRMNL](https://usetrmnl.com) plugin that displays an 8-hour weather forecast scraped from AccuWeather. Uses the webhook (push) model — a cron job scrapes the forecast and pushes it to your TRMNL device.

![half_horizontal layout](https://usetrmnl.com/images/plugins/trmnl--render.svg)

### Setup 
*Prerequisites*
- Go 1.24+
- Chrome or Chromium installed locally
- A [TRMNL](https://usetrmnl.com) device

*TRMNL*
1. Go to your [TRMNL dashboard](https://usetrmnl.com).
2. Create a new Private Plugin with strategy 'Webhook'.
3. Paste the contents of `templates/half_horizontal.liquid` into the Half Horizontal template field.
4. Save and copy the Plugin UUID.

*Server*
1. Clone this repo and run:
```bash
cd trmnl-weather
cp .env.example .env
```
2. Fill in your environment variables in .env.
3. Build and run the executable to verify everything works:
```bash
go build
./main
```

You should see a success message like `Pushed 8 hours of weather data to TRMNL (status 200)`.

4. Run the setup script to install a cron job (defaults to every 15 minutes):

```bash
chmod +x scripts/setup-cron.sh
./scripts/setup-cron.sh
```

Or specify a custom interval in minutes:

```bash
./scripts/setup-cron.sh 30
```

