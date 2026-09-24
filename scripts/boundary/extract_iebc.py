"""Extract Kenya's counties, constituencies and wards from the IEBC PDF
"Registered Voters per County Assembly Ward for the 2022 General Election"
(https://www.iebc.or.ke/docs/rov_per_caw.pdf) into db/seed/iebc_2022_wards.csv.

Usage:  python3 extract_iebc.py rov_per_caw.pdf ../../db/seed/iebc_2022_wards.csv
Needs:  pip install pdfplumber

The run fails loudly unless the extract has exactly 47 counties,
290 constituencies and 1,450 wards with contiguous codes, and the voter
total equals the PDF's printed total (22,102,532).
"""
import csv
import re
import sys

import pdfplumber

ROW = re.compile(r"^(\d{3}) (.+?) (\d{3}) (.+?) (\d{4}) (.+?) ([\d,]+)$")
EXPECTED_VOTERS = 22_102_532
SMALL_WORDS = {"wa", "la", "ya", "na", "cha", "kwa", "and", "of"}


def clean(name: str) -> str:
    """Fix known source typos: stray lowercase letters and a backslash."""
    return re.sub(r"\s+", " ", name.upper().replace("\\", "/")).strip()


def display(name: str) -> str:
    """UPPERCASE IEBC name -> readable form, e.g. ZIWA LA NG'OMBE -> Ziwa la Ng'ombe."""
    out, word_index = [], 0
    for part in re.split(r"([ /\-])", name):
        if part in (" ", "/", "-") or part == "":
            out.append(part)
            if part == "/":
                word_index = 0
            continue
        low = part.lower()
        if re.fullmatch(r"'?[a-z]'?", low):  # single letters: SOUTH C, MANYATTA 'B'
            out.append(part.upper())
        elif word_index > 0 and low in SMALL_WORDS:
            out.append(low)
        else:
            out.append(low[0].upper() + low[1:])
        word_index += 1
    return "".join(out)


def main(pdf_path: str, out_path: str) -> None:
    rows, unparsed = [], []
    with pdfplumber.open(pdf_path) as pdf:
        for page_no, page in enumerate(pdf.pages, 1):
            for line in (page.extract_text() or "").splitlines():
                line = line.strip()
                m = ROW.match(line)
                if m:
                    cc, cn, kc, kn, wc, wn, voters = m.groups()
                    rows.append((cc, clean(cn), kc, clean(kn), wc, clean(wn), int(voters.replace(",", ""))))
                elif not line.startswith(("REGISTERED VOTERS", "County Code", "Page ", "Total")):
                    unparsed.append((page_no, line))

    counties = {r[0] for r in rows}
    constituencies = {r[2] for r in rows}
    wards = [r[4] for r in rows]
    parents = {}
    for r in rows:
        parents.setdefault(r[2], set()).add(r[0])
    checks = {
        "no unparsed lines": not unparsed,
        "47 contiguous county codes": sorted(counties) == [f"{i:03d}" for i in range(1, 48)],
        "290 contiguous constituency codes": sorted(constituencies) == [f"{i:03d}" for i in range(1, 291)],
        "1450 unique contiguous ward codes": sorted(wards) == [f"{i:04d}" for i in range(1, 1451)],
        "each constituency in one county": all(len(v) == 1 for v in parents.values()),
        f"voter total {EXPECTED_VOTERS:,}": sum(r[6] for r in rows) == EXPECTED_VOTERS,
    }
    for name, ok in checks.items():
        print(("PASS " if ok else "FAIL ") + name)
    if not all(checks.values()):
        sys.exit(f"validation failed; unparsed: {unparsed[:5]}")

    with open(out_path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow([
            "county_code", "county_name", "county_display",
            "constituency_code", "constituency_name", "constituency_display",
            "ward_code", "ward_name", "ward_display", "registered_voters_2022",
        ])
        for cc, cn, kc, kn, wc, wn, voters in sorted(rows, key=lambda r: r[4]):
            w.writerow([cc, cn, display(cn), kc, kn, display(kn), wc, wn, display(wn), voters])
    print(f"wrote {len(rows)} wards to {out_path}")


if __name__ == "__main__":
    main(*sys.argv[1:3])
