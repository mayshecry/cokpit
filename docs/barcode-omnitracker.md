# Barcode & Omnitracker Integration

This document explains how the barcode system works for work orders, how it integrates with AFAS and Omnitracker, and lists all available API endpoints.

## Overview

The barcode system creates a unique barcode for each work order. When scanned with a phone or barcode scanner, it opens a mobile-friendly page showing all order information and provides a direct link to the Omnitracker ticket.

### Workflow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                             │
│  1. Order comes in from AFAS                                               │
│     └─> Debit number, customer, device info imported                       │
│                                                                             │
│  2. Picklist is created in Cockpit                                         │
│     └─> Internal work order with all details                               │
│                                                                             │
│  3. Barcode is generated for the work order                                │
│     └─> Unique barcode: CPT-{order_id}-{random}                            │
│     └─> QR code generated for the barcode                                  │
│                                                                             │
│  4. Barcode is printed and attached to physical work order                 │
│                                                                             │
│  5. Employee scans barcode with phone                                      │
│     └─> Opens scan page: /scan.html?code=CPT-XXX-XXX                       │
│     └─> Shows: debit, customer, Omnitracker ticket, device, asset, SI      │
│     └─> One-tap to open in Omnitracker                                     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Data Model

### Orders Table (New Columns)

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `debit_number` | TEXT | AFAS debit number | AFAS import |
| `customer_name` | TEXT | Customer name | AFAS import |
| `omnitracker_ticket` | TEXT | Omnitracker ticket reference | Manual/Integration |
| `device` | TEXT | Device type/model | AFAS import |
| `asset_number` | TEXT | Asset/serial number | AFAS import |
| `configuration` | TEXT | Configuration reference | Manual |
| `si_id` | INTEGER | Linked System Integration | Manual |
| `barcode` | TEXT | Unique barcode (CPT-XXX-XXX) | Auto-generated |

### Barcode Scans Table

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Primary key |
| `order_id` | INTEGER | Reference to order |
| `barcode` | TEXT | Scanned barcode value |
| `scanned_by` | TEXT | Username who scanned |
| `scan_type` | TEXT | Type of scan (view, check, etc.) |
| `device_info` | TEXT | User agent of scanning device |
| `created_at` | BIGINT | Timestamp |

## Barcode Format

Barcodes follow the format: `CPT-{order_id}-{random}`

Example: `CPT-42-839201`

The barcode encodes a URL: `https://your-domain.com/scan.html?code=CPT-42-839201`

## Omnitracker Integration

### Deep Link

When `OmnitrackerBaseURL` is configured, the scan page shows a direct link to the Omnitracker ticket:

```
https://omnitracker.example.com/ticket/TICKET-123
```

### Configuration

Set the Omnitracker base URL in your server configuration:

```go
cfg := api.ServerConfig{

## API Endpoints

### Barcode Management

#### Get Barcode

Returns the barcode for an order. If no barcode exists, returns empty string.

```
GET /api/v1/orders/{id}/barcode
```

**Permission:** `orders:view`

**Response:**
```json
{
  "barcode": "CPT-42-839201"
}
```

#### Generate Barcode

Generates a unique barcode for an order. If a barcode already exists, returns the existing one (unless `regenerate: true`).

```
POST /api/v1/orders/{id}/barcode
```

**Permission:** `orders:transition`

**Request Body:**
```json
{
  "regenerate": false
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `regenerate` | boolean | No | Force new barcode (old one stops working) |

**Response:**
```json
{
  "barcode": "CPT-42-839201"
}
```

#### Update Omnitracker Info

Updates the AFAS/Omnitracker fields on an order.

```
POST /api/v1/orders/{id}/omnitracker
```

**Permission:** `orders:transition`

**Request Body:**
```json
{
  "debitNumber": "DEB-2026-001",
  "customerName": "Acme Corp",
  "omnitrackerTicket": "TICKET-12345",
  "device": "Dell Latitude 5520",
  "assetNumber": "AST-001234",
  "configuration": "CFG-LAPTOP-STD",
  "siId": 5

#### Get Scan History

Returns the barcode scan history for an order.

```
GET /api/v1/orders/{id}/barcode/history
```

**Permission:** `orders:view`

**Response:**
```json
{
  "events": [
    {
      "id": 1,
      "orderId": 42,
      "barcode": "CPT-42-839201",
      "scannedBy": "alice",
      "scanType": "view",
      "deviceInfo": "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0...)",
      "createdAt": "2026-09-07T09:15:30Z"
    }
  ]
}
```

### Scan Endpoint

#### Scan Barcode

Looks up an order by barcode and records the scan event. This is the endpoint called when a barcode is scanned.

```
GET /api/v1/scan/barcode?code={barcode}&type={type}
```

**Permission:** `scan:use`

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `code` | string | Yes | Barcode value (e.g., CPT-42-839201) |
| `type` | string | No | Scan type (default: "view") |

**Response:**
```json
{
  "order": {
    "id": 42,
    "orderNumber": "ORD-2026-0042",
    "status": "Processing",
    "debitNumber": "DEB-2026-001",
    "customerName": "Acme Corp",
    "omnitrackerTicket": "TICKET-12345",
    "device": "Dell Latitude 5520",
    "assetNumber": "AST-001234",
    "configuration": "CFG-LAPTOP-STD",
    "siId": 5,
    "barcode": "CPT-42-839201"
  },
  "scanUrl": "/scan.html?code=CPT-42-839201",
  "omniUrl": "https://omnitracker.example.com/ticket/TICKET-12345"
}
```

## Frontend Components

### Order Detail Panel

The order detail panel in Cockpit shows a "Barcode" section with:

- Barcode value (monospace display)

## QR Code Generation

QR codes are generated client-side using [QRCode.js](https://davidshimjs.github.io/qrcodejs/) (loaded from CDN).

The QR code encodes the scan page URL:
```
https://your-domain.com/scan.html?code=CPT-42-839201
```

### Print QR Codes

To print QR codes for physical work orders:

1. Open the order detail in Cockpit
2. Click on the "Barcode" section
3. Right-click the QR code and select "Print"
4. Or use the browser's print function (Ctrl+P / Cmd+P)

## Database Migration

For existing databases, the migration adds the new columns automatically. If migration fails, run this SQL manually:

```sql
-- Add new columns to orders table
ALTER TABLE orders ADD COLUMN debit_number TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN customer_name TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN omnitracker_ticket TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN device TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN asset_number TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN configuration TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN si_id INTEGER;
ALTER TABLE orders ADD COLUMN barcode TEXT NOT NULL DEFAULT '';

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_orders_barcode ON orders(barcode);
CREATE INDEX IF NOT EXISTS idx_orders_omnitracker ON orders(omnitracker_ticket);

-- Create barcode scans table
CREATE TABLE IF NOT EXISTS barcode_scans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    barcode TEXT NOT NULL,
    scanned_by TEXT NOT NULL DEFAULT '',
    scan_type TEXT NOT NULL DEFAULT 'view',
    device_info TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_barcode_scans_order ON barcode_scans(order_id);
CREATE INDEX IF NOT EXISTS idx_barcode_scans_barcode ON barcode_scans(barcode);
```

## Security & Permissions

| Action | Required Permission |
|--------|-------------------|
| View barcode | `orders:view` |
| Generate barcode | `orders:transition` |
| Update Omnitracker info | `orders:transition` |

## Troubleshooting

### "no such column: barcode" error

Your database was created before the barcode feature was added. Either:

1. Delete and recreate the database (loses data):
   ```bash
   rm cockpit.db cockpit.db-wal cockpit.db-shm
   ```

2. Or run the manual migration SQL (see Database Migration section above)

### QR code not loading

Ensure the QRCode.js CDN is accessible. The script is loaded in `index.html`:
```html
<script src="https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js"></script>
```

### Omnitracker link not showing

Set the `OmnitrackerBaseURL` in your server configuration. Without this, the deep link won't be generated.

## Example: Full Workflow

```bash
# 1. Create an order
curl -X POST http://localhost:8080/api/v1/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"orderNumber": "ORD-2026-0042"}'

# 2. Update with AFAS/Omnitracker info
curl -X POST http://localhost:8080/api/v1/orders/1/omnitracker \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "debitNumber": "DEB-2026-001",
    "customerName": "Acme Corp",
    "omnitrackerTicket": "TICKET-12345",
    "device": "Dell Latitude 5520",
    "assetNumber": "AST-001234",
    "configuration": "CFG-LAPTOP-STD"
  }'

# 3. Generate barcode
curl -X POST http://localhost:8080/api/v1/orders/1/barcode \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}'

# Response: {"barcode": "CPT-1-839201"}

# 4. Scan the barcode (simulate)
curl "http://localhost:8080/api/v1/scan/barcode?code=CPT-1-839201" \
  -H "Authorization: Bearer $TOKEN"

# 5. View scan history
curl http://localhost:8080/api/v1/orders/1/barcode/history \
  -H "Authorization: Bearer $TOKEN"
```
| View scan history | `orders:view` |
| Scan barcode | `scan:use` |

## Integration with AFAS

To automatically import orders from AFAS with all fields populated:

1. Create the order via API or UI
2. Call `POST /api/v1/orders/{id}/omnitracker` with the AFAS data:

```bash
curl -X POST http://localhost:8080/api/v1/orders/42/omnitracker \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "debitNumber": "DEB-2026-001",
    "customerName": "Acme Corp",
    "device": "Dell Latitude 5520",
    "assetNumber": "AST-001234"
  }'
```

3. Generate the barcode:

```bash
curl -X POST http://localhost:8080/api/v1/orders/42/barcode \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}'
```
- QR code (scannable)
- "Generate Barcode" button (if no barcode)
- "View Scan Page" link
- "Regenerate" button (if barcode exists)

### Scan Page (`/scan.html`)

Mobile-friendly page that displays:

- Order number
- Debit number
- Customer name
- Omnitracker ticket (with deep link)
- Device
- Asset/Serial number
- Configuration
- SI reference
- Status

**Action Buttons:**
- "Open in Cockpit" - links to order detail in Cockpit
- "Open in Omnitracker" - deep link to Omnitracker ticket
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `debitNumber` | string | No | AFAS debit number |
| `customerName` | string | No | Customer name |
| `omnitrackerTicket` | string | No | Omnitracker ticket reference |
| `device` | string | No | Device type/model |
| `assetNumber` | string | No | Asset/serial number |
| `configuration` | string | No | Configuration reference |
| `siId` | integer | No | Linked System Integration ID |

**Response:** Full order object with updated fields
    OmnitrackerBaseURL: "https://omnitracker.example.com",
    // ... other config
}
```