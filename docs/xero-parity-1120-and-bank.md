# goXero ↔ Xero: паритет по выпискам банка и отчётности для 1120

**Дата:** 2026-09-11 · **Ветка:** `bonefish` (5caf8b1)
**Источники:** код goXero; живая Xero-демо `Demo Company (Global)` (`!!6Sp3`) и US-организация `Robris`
(обе доступны под выданным логином); Xero Central.

---

## 1. Главный вывод: Xero не делает 1120

Проверено на живой Xero:

* Поиск «1120» в каталоге отчётов (`reporting.xero.com`) — **No results found**. И в Global-, и в US-организации.
* Единственный отчёт-формуляр в Xero US — **1099 Report** (NEC/MISC): правила «счёт → коробка 1099»,
  W-9 management, e-filing в IRS. Это *информационная* отчётность, не декларация.
* 1120 в Xero не формируется вообще. Бухгалтер собирает её вне Xero — из P&L, Balance Sheet,
  Trial Balance, General Ledger.

**Следствие для проекта.** «1:1 с Xero» по 1120 — это не «сделать генератор формы», а
воспроизвести набор отчётов, из которых 1120 собирается, и сделать их *корректными и
периодно-точными*. Плюс — фиксированная сверка с IRS-разметкой (Schedule L: beginning/end of year).

---

## 2. Блокер №1: P&L, Balance Sheet и Trial Balance игнорируют даты

Это ломает саму возможность подготовки 1120: декларация считается за конкретный налоговый год.

`internal/repository/report.go` — во всех трёх отчётах фильтр по дате стоит в `ON`-условии
**второго** `LEFT JOIN`, поэтому не фильтрует основные строки:

```
FROM accounts a
LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id          -- ← все строки за всю историю
LEFT JOIN gl_journals      j ON j.journal_id = l.journal_id
                             AND j.organisation_id = a.organisation_id
                             AND j.journal_date <= $2                -- ← не фильтрует l
```

`j` не используется ни в `SELECT`, ни в `WHERE` — это «фильтр», который не фильтрует.
`SUM(l.net_amount)` складывает строки за всё время.

| Отчёт | Строка с багом | Эффект |
|---|---|---|
| `TrialBalance` | `report.go:55` | `?date=` не действует |
| `ProfitAndLoss` | `report.go:108` | `?fromDate/?toDate` не действуют |
| `BalanceSheet` | `report.go:177` | `?date=` не действует |
| `BankSummary` | `report.go:417-420` | ✅ корректно (`CASE WHEN j.journal_date`) |
| `JournalFeed` | `report.go:328` | ✅ корректно (`WHERE`) |
| `SalesTaxByRate` | `report.go:274` | ✅ корректно (фильтр по `invoices.date`) |

Починка — один предикат: перенести условие в `WHERE` (или `JOIN ... ON ... AND l.journal_id = j.journal_id
AND j.journal_date BETWEEN ...` с `INNER JOIN`). Без этого любые «отчёты для 1120» показывают
данные за всё время — цифры неверные, а не просто неполные.

---

## 3. Банк: что есть в Xero и чего нет в goXero

### 3.1 Xero — автоматические выписки (bank feed)

| Возможность | Xero | goXero |
|---|---|---|
| Подключение банка (OAuth/consent) | ✅ прямая интеграция + партнёры | ✅ `bank-feed` (GoCardless BAD, PSD2) |
| Список банков/институтов | ✅ | ✅ `/bank-feeds/institutions` |
| Синхронизация | ✅ автоматически + вручную | ⚠️ только вручную `POST /connections/:id/sync`, по крону ничего нет |
| Привязка счёта фида к счёту в плане счетов | ✅ | ✅ `PUT /bank-feeds/accounts/:id` |
| Дедупликация строк | ✅ | ✅ `UNIQUE(feed_account_id, provider_tx_id)` |
| **Баланс строки выписки (running balance)** | ✅ колонка `Balance` в «Bank statements» | ❌ в `bank_feed_statement_lines` нет поля balance (только `bank_feed_accounts.balance`) |
| Колонка `Source` (Bank feed / Imported) | ✅ | ❌ |
| Статус «Auto-reconciled / Unreconciled» | ✅ | ❌ |
| **Разбор входящих строк (inbox): просмотр / импорт / игнор** | ✅ полноценный UI | ❌ **API есть, UI нет вообще** (`statement-lines` не упоминается ни в `web/src`) |

### 3.2 Xero — ручные выписки (manual import)

Мастер в Xero: `Import bank statement` → **3 шага** (`Upload` → `Import settings` → `Review`).

* Форматы: **OFX (рекомендуется), QFX, QIF, QBO, PDF, CSV** + скачиваемый шаблон CSV.
* Шаблон CSV Xero (декодирован из приложения): `*Date,*Amount,Payee,Description,Reference,Check Number`.
* Шаг 2 — **маппинг колонок**: «Column heading from CSV file → Assign to field in Xero»
  (Transaction Date, Transaction Amount, Payee, Description, No field), опция
  «Don't import the first line because they're column headings», превью транзакций.
* Шаг 3 — Review: «N transaction(s) are ready to import», предупреждение
  «Xero will check for duplicates when you complete the import».
* Плюс: «Import a precoded CSV bank statement so it's reconciled automatically when you import it».

**В goXero этого нет вообще** — ни парсера OFX/QIF/CSV, ни эндпоинта, ни UI.

### 3.3 Xero — сверка (reconcile)

Экран `BankRec.aspx`: две панели (слева строки выписки, справа совпадения в Xero), на строку —
`OK / Match / Create / Transfer / Discuss / Find & Match`, поиск по Payee/Amount/Reference/
Description/Cheque No./Analysis Code, `Filter`, `Compact view`, `Auto-reconcile ON/OFF`,
`Reconciliation Report`, `Manage Account`. Вкладки: `Reconcile (N)`, `Cash coding`,
`Bank statements`, `Account transactions`, `Reconcile period`.

В `web/src/routes/app/accounting/bank-accounts/[id]/+page.svelte` всё это свёрстано, но не работает:

| Элемент UI | Строка | Состояние |
|---|---|---|
| Reconcile | `:209` → `markReconciled()` `:90` | ⚠️ **баг**: вызывает `bankTransactionApi.create({...tx, IsReconciled:true})`, а `BankTransactionRepository.Create` всегда `INSERT` → создаётся **дубликат** транзакции, а не отметка сверки |
| Cash coding (Uncheck all / Apply rule / Save & Reconcile All) | `:225-227` | ⛔ `disabled` |
| Bank statements → Import statement | `:273` | ⛔ `disabled` |
| Reconcile period → Create period | `:370` | ⛔ `disabled` |
| Auto-reconcile | — | ❌ отсутствует (нет ни логики, ни эндпоинта) |
| Match / Create / Transfer / Find & Match | — | ❌ отсутствуют; импорт строки — только «создать транзакцию» |

### 3.4 Банковские правила

| Возможность | Xero | goXero |
|---|---|---|
| CRUD правил (spend/receive/transfer) | ✅ | ✅ `internal/repository/bank_rule.go` |
| Условия + фикс./процентные аллокации | ✅ | ✅ модель `BankRuleDefinition` описана |
| Порядок применения (drag&drop) | ✅ | ⚠️ поля порядка нет |
| **Движок применения правил** | ✅ (в т.ч. при импорте и авто-сверке) | ❌ **отсутствует**: в `bank_rule.go` только `List/GetByID/Create/Update/Delete` |

Импорт строки (`internal/handlers/bank_feed.go`, `ImportStatementLine`) требует вручную переданный
`AccountCode` на **каждую строку по одной**, правила не применяются, нет ни bulk-импорта, ни матчинга
с существующими транзакциями (`Match`/`Find & Match`).

---

## 4. Отчёты: сравнение каталогов

Каталог goXero (`web/src/lib/reports-catalog.ts`) списан с Xero, но у большинства записей `href: null`
(только заглушка UI). Реально работает 12 эндпоинтов:

Trial Balance · P&L · Balance Sheet · Aged Receivables · Aged Payables · Bank Summary · Cash Summary ·
Executive Summary · Budget Summary · BAS/Sales Tax · Journal Report · Invoice Summary.

Плюс есть 404-ссылка: `/app/reports/account-transactions` дёргает `/api/v1/reports/account-transactions`,
которого нет в `internal/router/router.go`.

### Что из этого реально нужно для 1120

| Отчёт | Нужен для 1120 | Xero | goXero |
|---|---|---|---|
| Income Statement (P&L) с периодами | ✅ основной | ✅ | ⚠️ есть, но **дата не работает** (§2) |
| Balance Sheet beginning/end of year (Schedule L) | ✅ | ✅ | ⚠️ есть, но **дата не работает**, компартива нет |
| Trial Balance на дату | ✅ | ✅ | ⚠️ есть, но **дата не работает** |
| General Ledger (Summary/Detail) | ✅ | ✅ | ⚠️ Detail подменён `journal-report` |
| Journal Report | ✅ | ✅ | ✅ работает |
| Depreciation Schedule / Fixed Asset Reconciliation | ✅ (Form 4562) | ✅ | ❌ `fixed-assets` = `ComingSoon` |
| Tax Reconciliation | ⚠️ полезно | ✅ | ❌ нет |
| 1099 Report (NEC/MISC) | ⚠️ сопутствующее | ✅ (US) | ❌ `/app/tax/1099` = `ComingSoon` |
| Sales Tax Report | ⚠️ | ✅ | ✅ (`BAS`) |
| **Form 1120 / Schedule L / M-1 / M-2** | ✅ | ❌ **нет и в Xero** | ❌ |
| Компаративные периоды (год к году) | ✅ | ✅ | ❌ |
| Экспорт отчёта (PDF/CSV/Google Sheets) | ✅ | ✅ | ❌ |

---

## 5. План работ (приоритет)

**P0 — без этого 1120 не собрать**
1. Починить фильтр по датам в `TrialBalance` / `ProfitAndLoss` / `BalanceSheet` (§2). Регрессионный тест на `fromDate/toDate`.
2. Компаративные периоды в P&L и Balance Sheet (beginning/end of year) — без них Schedule L не заполнить.
3. Экспорт отчётов (хотя бы CSV/PDF).

**P1 — «выписки 1:1»**
4. UI для входящих строк фида (`statement-lines`): список, импорт, игнор (API уже готов).
5. Ручной импорт выписок: парсеры OFX/QFX/QIF/CSV + мастер из 3 шагов + шаблон CSV `*Date,*Amount,Payee,Description,Reference,Check Number`.
6. Движок банковских правил + порядок применения; применение при импорте.
7. Экран сверки: `Match` / `Create` / `Find & Match` / `Transfer`; убрать баг с дубликатом (§3.3).

**P2 — доводка до Xero**
8. `Balance` и `Source` в строках выписки, статус Auto-reconciled/Unreconciled.
9. Авто-сверка + Cash coding + Reconcile period.
10. Плановый синк фида (сейчас только ручной `sync`).
11. Deferred: 1099, Depreciation Schedule / Fixed Assets.

---

## 6. Прочие найденные дефекты

* `web/src/routes/app/accounting/bank-accounts/[id]/+page.svelte:90` — «Reconcile» создаёт дубликат
  банковской транзакции (см. §3.3).
* `/app/reports/account-transactions` — 404 (эндпоинт не зарегистрирован).
* `/app/reports/general-ledger-detail` рендерит `journal-report`, а не GL Detail.
* Каталог отчётов обещает ~80 отчётов, работает 12 — стоит либо скрыть неготовые, либо помечать.
