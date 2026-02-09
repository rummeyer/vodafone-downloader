# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-02-09

### Added

- Initial release
- Login to Vodafone MeinVodafone portal
- Download Mobilfunk invoices
- Download Kabel invoices
- Send invoices via email with PDF attachments
- Configuration via `config.json`
- In-memory PDF handling (no disk I/O)
- Progress messages with month/year info
- Headless Chrome automation via chromedp

### Technical

- Extracted `navigateToInvoicePage()` for contract navigation
- Extracted `capturePDF()` for PDF blob interception
- Contract type mapping for easy extensibility
