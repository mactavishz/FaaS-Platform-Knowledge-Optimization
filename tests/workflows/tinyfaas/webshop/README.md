# Webshop Function Workflow (Entry = `frontend`)

This document describes the webshop workflow deployed under `webshop/` for the tinyFaaS platform.

## Functions

- **Workflow steps:** `webshop/*/handler.js`

| Function              | Directory              | Role                                                            |
|-----------------------|------------------------|-----------------------------------------------------------------|
| `frontend`            | `frontend/`            | BFF entry point — routes requests to downstream functions       |
| `checkout`            | `checkout/`            | Checkout orchestrator — full purchase flow                      |
| `addcartitem`         | `addcartitem/`         | Adds a single product to a user's cart                          |
| `emptycart`           | `emptycart/`           | Removes all items from a user's cart                            |
| `getcart`             | `getcart/`             | Retrieves all cart items for a user                             |
| `cartstorage`         | `cartstorage/`         | Cart key-value store backed by Supabase (`webshop_cart` table)  |
| `listproducts`        | `listproducts/`        | Returns the full hardcoded product catalogue (11 items)         |
| `getproduct`          | `getproduct/`          | Retrieves a single product by ID                                |
| `searchproducts`      | `searchproducts/`      | Searches product names and descriptions for a query string      |
| `listrecommendations` | `listrecommendations/` | Returns up to 2 category-based product recommendations          |
| `currency`            | `currency/`            | Converts a monetary amount between two currencies               |
| `supportedcurrencies` | `supportedcurrencies/` | Returns the list of all supported currency codes                |
| `getads`              | `getads/`              | Returns 2 random advertisements                                 |
| `shipmentquote`       | `shipmentquote/`       | Calculates shipping cost ($1.50 per item unit)                  |
| `shiporder`           | `shiporder/`           | Simulates order fulfilment (CPU load, returns tracking ID)      |
| `payment`             | `payment/`             | Simulates payment processing (CPU load, returns transaction ID) |
| `email`               | `email/`               | Simulates sending a confirmation email (CPU load via worker)    |

> **Note:** `payment` is part of the reference architecture but is not called by the current
> workflow graph. It is included for completeness and future extension.

## High-Level Call Graph

Legend:

- `sync` — the caller awaits a result before continuing.
- `async` — fire-and-forget; the caller does not wait for the result.

Current frontend behavior:

- `operation="addcart"` is synchronous from `frontend` to `addcartitem`
- `operation="checkout"` is synchronous from `frontend` to `checkout`

```text
frontend (entry, dispatches by operation)

  operation="get"
    |-sync-> supportedcurrencies
    |-sync-> listproducts
    |-sync-> currency          (once per product for price conversion)
    |-sync-> getads
    |-sync-> getcart
    |          |-sync-> cartstorage  [Supabase: SELECT webshop_cart WHERE user_id=?]
    |-sync-> listrecommendations
                 |-sync-> listproducts

  operation="cart"
    |-sync-> getcart
    |          |-sync-> cartstorage  [Supabase: SELECT webshop_cart WHERE user_id=?]
    |-sync-> shipmentquote

  operation="addcart"
    |-sync->  addcartitem
    |           |-sync->  cartstorage  [Supabase: UPSERT INTO webshop_cart]
    |-sync->  getcart
                |-sync-> cartstorage  [Supabase: SELECT webshop_cart WHERE user_id=?]

  operation="checkout"
    |-sync->  checkout
                |-sync->  getcart
                |           |-sync-> cartstorage  [Supabase: SELECT webshop_cart WHERE user_id=?]
                |-sync->  listproducts
                |-sync->  currency  (once per cart item + once for shipment)
                |-sync->  shipmentquote
                |-async-> shiporder   (CPU sim: Sieve of Eratosthenes)
                |-async-> email       (CPU sim: worker thread atan*tan loop)
                |-async-> emptycart
                            |-async-> cartstorage  [Supabase: DELETE FROM webshop_cart WHERE user_id=?]

  operation="emptycart"
    |-async-> emptycart
                |-async-> cartstorage  [Supabase: DELETE FROM webshop_cart WHERE user_id=?]
```

## Supabase I/O Summary

### Tables

- `public.webshop_cart`

### Schema

```sql
public.webshop_cart (
  user_id    text        not null,
  item_id    text        not null,
  quantity   integer     not null default 1,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (user_id, item_id)
)
```

### Reads

- `cartstorage` (operation=`get`) selects all rows where `user_id = ?`

### Writes

- `cartstorage` (operation=`add`) upserts `(user_id, item_id, quantity)` — on conflict updates quantity
- `cartstorage` (operation=`empty`) deletes all rows where `user_id = ?`

## Setup

### Prerequisites

- A running tinyFaaS instance accessible at `http://tinyfaas.com`
- A Supabase project with the `webshop_cart` table created (see below)
- `faas-cli` installed and configured for the tinyFaaS platform

### 1. Create the database table

From the repository root:

```bash
export SUPABASE_DB_URL="<your-supabase-db-host>"
bash tests/supabase/scripts/db_webshop_up.sh
```

This creates `public.webshop_cart` with the composite primary key, index, RLS policy,
and the `updated_at` trigger.

To reset data between test runs:

```bash
bash tests/supabase/scripts/db_webshop_truncate.sh
```

To drop the table entirely:

```bash
bash tests/supabase/scripts/db_webshop_down.sh
```

### 2. Configure environment variables

Create `.env.yaml` from the example template:

```bash
cd tests/workflows/tinyfaas/webshop
cp .env.yaml.example .env.yaml
```

Fill in your Supabase credentials in `.env.yaml`:

```yaml
environment:
  SUPABASE_URL: "https://<project-ref>.supabase.co"
  SUPABASE_KEY: "<your-supabase-anon-or-service-role-key>"
```

`.env.yaml` is git-ignored and must never be committed.

Only the `cartstorage` function needs these credentials. The `environment_file` field in `stack.yml` injects them only for that function.

### 3. Deploy

```bash
# From repo root:
faas-cli deploy --platform tinyfaas -f ./tests/workflows/tinyfaas/webshop/stack.yml
```

### 4. Invoke the workflow

Send a POST request to the `frontend` function via the tinyFaaS gateway:

```bash
# Homepage (get all products + cart + ads + recommendations)
curl -X POST http://tinyfaas.com/fn/webshop-frontend \
  -H 'Content-Type: application/json' \
  -d '{"operation":"get","userId":"user1","currency":"USD"}'

# View cart with shipping cost
curl -X POST http://tinyfaas.com/fn/webshop-frontend \
  -H 'Content-Type: application/json' \
  -d '{"operation":"cart","userId":"user1"}'

# Add item to cart (productId "3" = Computer Mouse, quantity 2)
curl -X POST http://tinyfaas.com/fn/webshop-frontend \
  -H 'Content-Type: application/json' \
  -d '{"operation":"addcart","userId":"user1","productId":"3","quantity":2}'

# Checkout
curl -X POST http://tinyfaas.com/fn/webshop-frontend \
  -H 'Content-Type: application/json' \
  -d '{"operation":"checkout","userId":"user1","currency":"EUR","address":{"street":"123 Main St"}}'

# Empty cart
curl -X POST http://tinyfaas.com/fn/webshop-frontend \
  -H 'Content-Type: application/json' \
  -d '{"operation":"emptycart","userId":"user1"}'
```

You can also invoke individual functions directly:

```bash
# List all products
curl -X POST http://tinyfaas.com/fn/webshop-listproducts \
  -H 'Content-Type: application/json' -d '{}'

# Convert $24.99 USD to EUR
curl -X POST http://tinyfaas.com/fn/webshop-currency \
  -H 'Content-Type: application/json' \
  -d '{"from":{"currencyCode":"USD","units":24,"nanos":990000000},"toCode":"EUR"}'

# Get shipping quote for 3 items
curl -X POST http://tinyfaas.com/fn/webshop-shipmentquote \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"quantity":2},{"quantity":1}]}'

# Search products
curl -X POST http://tinyfaas.com/fn/webshop-searchproducts \
  -H 'Content-Type: application/json' \
  -d '{"query":"Programmer"}'
```
