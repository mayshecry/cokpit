# Functioneel ontwerp — Klant → Project → System Integration (SI)

**Systeem:** Order Cockpit (cockpit-v2) · **Scope:** klant 94828 met vier projecten; SI's per klant/project met lifecycle, autorisatie en koppeling. · **Versie:** 1.0 · **Datum:** 2026-09-04

---

## 1. Uitgangspunten

- Een **klant** (klantnummer 94828) is de hoogste organisatorische entiteit: administratie, rapportage, facturatie en autorisatie lopen erdoorheen.

- Onder de klant vallen **vier projecten** die afzonderlijk herkenbaar én samen onder de ene klant rapporteerbaar zijn.-
- Een **System Integration (SI)** is een koppeling/integratie tussen systemen met een eigen lifecycle van aanvraag tot uitfasering.te
- Rollen: `viewer`, `operator`, `qc`, `npi` (nieuw) en `admin`.

---

## 2. Datastructuur: Klant → Project → SI

```
        KLANT (94828)
            │  (1 ── n)
            ▼
        PROJECT ──────────► 1 primair project per SI
            │  ▲
            │  │ (n ── m, alleen binnen dezelfde klant)
            ▼  │
         SI (system_integrations)
```

| Tabel | Belangrijkste velden | Toelichting |
|---|---|---|
| `customers` | `id`, `number` (UNIQ), `name`, `created_at` | De klant. Eén rij voor 94828. |
| `projects` | `id`, `customer_id` (FK), `code` (UNIQ binnen klant), `name`, `description`, `created_at`, `updated_at` | Vier projecten onder klant 94828. |
| `system_integrations` | `id`, `code` (UNIQ), `name`, `description`, `status`, `version`, `environment`, `primary_project_id` (FK), `created_by`, `created_at`, `updated_at` | Eén SI. Status = lifecycle, version = versiebeheer, environment = TEST/ACCEPTATIE/PRODUCTIE. |
| `si_project_links` | `si_id`, `project_id`, `is_primary` (PK: si_id+project_id) | Koppeling SI ↔ project(en; `is_primary`=1 voor het eigenaarproject. |
| `si_events` | `id`, `si_id` (FK), `action`, `from_status`, `to_status`, `version`, `note`, `performed_by`, `timestamp` | Onveranderlijke audittrail van alle SI-acties. |

### Voorbeeld voor klant 94828

| Klant | Project (code) | SI (code) | Status | Koppeling |
|---|---|---|---|---|
| 94828 | P-001 (Ordering) | SI-0001 (EDI inkooporders) | Live | primair: P-001; deelname: P-003 |
| 94828 | P-002 (Facturatie) | SI-0002 (Factuuruitgifte) | Testing | primair: P-002 |
| 94828 | P-003 (Magazijn) | SI-0003 (WMS-koppeling) | Development | primair: P-003 |
| 94828 | P-004 (Rapportage) | SI-0004 (BI-extract) | Requested | primair: P-004 |

**Rapportage:** filter op klantnummer toont **alle** SI's van alle projecten van 94828 (gezamenlijk). Filter op project toont alleen SI's waar dat project primair óf deelnemend aan is (afzonderlijk), met markering van het primair project.



## 3. SI-lifecycle

### 3.1 Statussen

| Status | Label (NL) | Betekenis |
|---|---|---|
| `Requested` | Aangevraagd | NPI heeft de koppeling aangevraagd; nog geen ontwerp. |
| `Design` | In ontwerp | Functioneel/technisch ontwerp wordt gemaakt. |
| `Development` | In ontwikkeling | Bouw/implementatie van de koppeling. |
| `Testing` | In test | Testfase (test/acceptatie-omgeving). |
| `Live` | Live / actief | In productie, operationeel. |
| `Change` | Gewijzigd (in wijzigingscyclus) | Een live SI wordt gewijzigd; staat op slot voor de wijzigingscyclus. |
| `Retired` | Uitgefaseerd | Buiten gebruik genomen; gegevens blijven behouden. |
| `Archived` | Gearchiveerd | Eindtoestand; alleen nog raadplegen. |
| `Cancelled` | Geannuleerd | Aanvraag afgewezen/ingetrokken vóór Live. |

### 3.2 Toegestane overgangen

```
Requested ──► Design ──► Development ──► Testing ──► Live ──► Retired ──► Archived
   │            │            │  ▲           │  ▲      │   ▲        (terminal)
   │            │            ▼  │           ▼  │      ▼   │
   └─►Cancelled └─►Cancelled  (Design)     (Development) Change
                                                       │    │
                                                       ▼    ▼
                                                    Development  Retired
```

| Van | Naar | Opmerking |
|---|---|---|
| `Requested` | `Design`, `Cancelled` | |
| `Design` | `Development`, `Cancelled` | |
| `Development` | `Testing`, `Design` | Terug naar ontwerp bij grote herziening. |
| `Testing` | `Live`, `Development` | Terug bij defecten. |
| `Live` | `Change`, `Retired` | `Change` = wijzigingscyclus starten. |
| `Change` | `Development`, `Retired` | Wijziging in uitvoering, of alsnog uitfaseren. |
| `Retired` | `Archived` | |
| `Archived` | — | Terminal (eindtoestand). |
| `Cancelled` | — | Terminal. |

**Wie mag overgangen uitvoeren:** alleen **NPI** (en `admin` als beheerfallback). `viewer`/`operator`/`qc` mogen SI's alleen inzien.**

### 3.3 Versiebeheer

- Elke SI heeft een `version` (start 0).
- Bij elke overgang **naar `Live`** wordt `version` met 1 verhoogd (1e go-live = v1, na wijzigingscyclus = v2, …).
- Een `Live → Change`-cyclus markeert een geplande nieuwe versie; pas bij terugkeer naar `Live` wordt de versie verhoogd.
- Elke actie wordt als `si_events`-regel vastgelegd (audittrail,, zodat vorige versies herleidbaar zijn.

### 3.4 Omgevingen

`environment` is een eigenschap van de SI (niet een status): `TEST`, `ACCEPTATIE`, `PRODUCTIE`. Standaard `PRODUCTIE`; beheerbaar door NPI.

---

## 4. Autorisatiematrix (rol × handeling)

| Handeling | viewer | operator | qc | **npi** | admin |
|---|:--:|:--:|:--:|:--:|:--:|
| SI's inzien (lijst/detail) | ✓ | ✓ | ✓ | ✓ | ✓ |
| Audittrail SI inzien | ✓ | ✓ | ✓ | ✓ | ✓ |
| SI aanmaken | ✗ | ✗ | ✗ | **✓** | ✓ |
| SI wijzigen (metadata/omgeving) | ✗ | ✗ | ✗ | **✓** | ✓ |
| Lifecycle-overgang (incl. wijziging) | ✗ | ✗ | ✗ | **✓** | ✓ |
| Uitfaseren / archiveren | ✗ | ✗ | ✗ | **✓** | ✓ |
| Projecten koppelen/ontkoppelen aan SI | ✗ | ✗ | ✗ | **✓** | ✓ |
| Project aanmaken | ✗ | ✗ | ✗ | **✓** | ✓ |
| Klant aanmaken | ✗ | ✗ | ✗ | ✗ | ✓ |

> NPI is de enige "schrijfrol" voor SI's; andere rollen zijn strikt lezend/gebruikend. Admin is een technische beheerfallback met alle rechten.

---

## 5. Koppeling SI ↔ project (keuze)

**Besluit:** een SI hangt aan **exact één primair project** (het beherende/eigenaarproject — verantwoordelijk voor beheer, kosten en doorontwikkeling), én kan optioneel aan **meerdere secundaire projecten van dezelfde klant** hangen (gedeeld gebruik.



- **Waarom niet strikt 1-op-1:** een koppeling wordt vaak door meerdere projecten van dezelfde klant gebruikt; dan hoeft de SI maar één keer beheerd te worden.

- **Waarom niet volledig vrij** (elke SI aan elke klant): dat doorbreekt de administratieve scheiding en autorisatie tussen klanten.vis
- **Integriteit:** alle gekoppelde projecten moeten onder **dezelfde klant** vallen als het primair project (multi-tenancy-speaking).
- **Zichtbaarheid:** in de SI-detailweergave wordt het primair project gemarkeerd; filter op klant toont alle SI's van de klant.vis

---

## 6. Aandachtspunten / risico's

1. **Eén NPI-schrijfrol = geen 4-ogen-principe.** Alleen NPI kan wijzigen/uitfaseren. Overweeg een aparte "goedkeurder"-rol of een verplichte goedkeuringsstap voor `Live` en `Retired` (change advisory board;. Nu bewust eenvoudig gehouden.

2. **Rollen zijn globaal, niet per klant/project.** Elke NPI-gerechtigde kan alle SI's van alle klanten beheren. Voor strikte scheiding is project-scoped autorisatie nodig (buiten deze scope.）
3. **Geen harde verwijdering van SI's.** Uitfaseren/archiveren i.p.v. delete, om de audittrail en historie intact te houden.te
4. **Wijzigingen aan Live SI** lopen verplicht via `Change`-cyclus, zodat productiewijzigingen beheerst en versioneerd gebeuren.vis
5. **Versiebeheer is teller + audittrail,** geen volledig config-beheer (geen snapshots van configuratie). Voor volledige herstelbaarheid is een config-diff/rollback nodig.

6. **Koppelingsintegriteit:** secundaire projecten moeten altijd onder dezelfde klant vallen; dit moet op API-niveau én in de UI afgedwongen worden.vis
7. **Multi-tenancy / privacy:** alle klanten delen één codebase; rapportage en autorisatie moeten klantscheiding waarborgen.vis
8. **Unieke codes:** klantnummer, projectcode (binnen klant) en SI-code moeten uniek zijn om verwarring te voorkomen.vis

---

## 7. Technische realisatie (cockpit-v2)

- Domein: `pkg/si` (statusen, transitietabel, typen, fouten) — zelfde patroon als `pkg/order`.
- Rollen/permissies: `pkg/auth` krijgt rol `npi` + permissies `si:list`, `si:view`, `si:create`, `si:update`, `si:transition`, `si:projects`, `si:manage`.
- Opslag: `pkg/db` — tabellen uit §2 + store-methoden + seed voor klant 94828.

- API: `pkg/api` — REST-eindpunten (zie onderstaande tabel,, met `requirePerm`-checks.
- UI: dashboardpagina voor klant/project/SI met lifecycle- en audittrail-weergave.



### API-eindpunten

| Methode | Pad | Permissie |
|---|---|---|
| GET | `/api/v1/customers` | `si:list` |
| POST | `/api/v1/customers` | `si:manage` (admin) |
| GET | `/api/v1/customers/{number}/projects` | `si:list` |
| POST | `/api/v1/projects` | `si:create` (NPI/admin) |
| GET | `/api/v1/sis` (filter: `customer`, `project`, `status`) | `si:list` |
| POST | `/api/v1/sis` | `si:create` (NPI/admin) |
| GET | `/api/v1/sis/{id}` | `si:view` |
| POST | `/api/v1/sis/{id}` | `si:update` (NPI/admin) |
| POST | `/api/v1/sis/{id}/transition` | `si:transition` (NPI/admin) |
| POST | `/api/v1/sis/{id}/projects` | `si:projects` (NPI/admin) |
| GET | `/api/v1/sis/{id}/events` | `si:view` |
| POST | `/api/v1/sis/demo` | `si:manage` (seed 94828) |