# SQL fixtures (development & testing)

These migrations live **outside** the core schema chain. They are applied only when you run:

```bash
./scripts/migrate dev up
```

Core schema + seed (`migrations/*.sql`) still use `./scripts/migrate up`.

Goose stores fixture versions in table **`goose_fixtures`** (separate from `goose_db_version`) so file names can use `00001`, `00002`, … without colliding with core migrations.

## Stable UUIDs (RFC 4122, generated with `uuidgen`)

| Entity        | UUID |
|---------------|------|
| Organisation “Fixture Labs LLC” | `72590a0d-deb9-4fcc-a05a-e40fb47afc43` |
| User `fixture-dev@goxero.test` (password `admin123`) | `f8584d07-ec38-488b-9521-64da6fae19ee` |
| Contact “Fixture Customer Inc” | `0945278a-c8d8-457b-8a94-8900d7b94e21` |
| Tax rate “Tax on Sales” | `6e64bbbf-c2c5-46fa-872f-1357c642c0a6` |
| Item `FIXTURE-SKU` | `4861bfeb-eb16-4f2d-82da-55a3f4a36fbf` |
| Accounts 090 / 200 / 610 | `ab2d610b-2875-4693-bd21-552b2ee4b86d`, `3f5f7b84-bf88-45f6-b199-8591b1d6770d`, `41e85ef9-33df-4838-8434-3642efafb8be` |
| Draft invoice `FIX-1001` | `1eb8691d-450a-4b89-a07e-9c0e8d007cd0` |
| Invoice line item | `cb97aa93-5e0d-4ae2-80ee-02c2d81dd4f7` |

## Commands

| Command | Effect |
|---------|--------|
| `./scripts/migrate dev up` | Core migrations up, then all fixture migrations |
| `./scripts/migrate dev down` | Roll back **one** fixture step if any applied; otherwise one core step |
| `./scripts/migrate dev reset` | `goose_fixtures` reset, then core `reset` |
| `./scripts/migrate dev status` | Core status, then fixture status |
| `./scripts/migrate dev version` | Core version, then fixture version |
| `./scripts/migrate dev up-to V` | Core `up-to V`, then fixtures `up` |
| `./scripts/migrate dev down-to V` | Fixtures `reset`, then core `down-to V` |

Adding a new fixture: create `migrations/fixtures/00003_name.sql` with `+goose Up` / `+goose Down` sections.
