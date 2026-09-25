# Seed data

## `iebc_2022_wards.csv` — administrative boundaries

47 counties, 290 constituencies and 1,450 County Assembly Wards, one row per ward.

- **Source:** Independent Electoral and Boundaries Commission (IEBC),
  *Registered Voters per County Assembly Ward for the 2022 General Election*,
  <https://www.iebc.or.ke/docs/rov_per_caw.pdf> (46 pages).
- **Codes** are IEBC's official codes: county `001`–`047`, constituency `001`–`290`,
  ward `0001`–`1450`. Treat them as stable identifiers for the boundary set.
- **Boundaries** are the 2012 delimitation used in the 2013, 2017 and 2022 elections.
  A new IEBC boundaries review will change these; import it as a new boundary version.
- **Regenerate:** `python3 scripts/boundary/extract_iebc.py rov_per_caw.pdf db/seed/iebc_2022_wards.csv`.
  The script refuses to write unless the counts, contiguous codes, one-county-per-constituency
  and the PDF's printed voter total (22,102,532) all check out.

Columns:

| Column | Meaning |
|---|---|
| `*_code` | IEBC code (zero-padded, keep as text) |
| `*_name` | IEBC name, uppercase — canonical, used for matching |
| `*_display` | Readable name for the UI, e.g. `Ziwa la Ng'ombe` |
| `registered_voters_2022` | Registered voters in the ward (2022 register) |

Corrections applied to the source (typos in the PDF):

| Ward | PDF text | Stored as |
|---|---|---|
| 0445 | `NJABINI\KIBURU` | `NJABINI/KIBURU` |
| 0888 | `OLOlMASANI` | `OLOLMASANI` |
| 0929 | `EWUASO OoNKIDONG'I` | `EWUASO OONKIDONG'I` |

Other public datasets were checked and rejected as a seed source:
`davidamunga/kenya-locations` has 1,448 wards (missing 0737 and 1344), 58 codes
shifted onto the wrong ward, and pre-2022 constituency names (Mbita, Suba).

Load it with `make db-seed` (runs `go run ./cmd/seed`). The seed is idempotent and
transactional: it commits only if `admin_units` then holds exactly 1 national unit,
47 counties, 290 constituencies and 1,450 wards. `make run-api` seeds automatically.
