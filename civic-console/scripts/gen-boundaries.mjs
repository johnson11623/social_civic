// Builds src/data/iebc-2022.json from the IEBC seed (db/seed/iebc_2022_wards.csv),
// the same data the API loads. The web app ships it as a lazily imported,
// content-hashed chunk (~15 KB gzipped, cached for good), so finding a ward
// during sign-up needs no request per keystroke. Run: pnpm gen:boundaries
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const csv = readFileSync(resolve(here, "../../db/seed/iebc_2022_wards.csv"), "utf8").trim().split("\n");
const header = csv[0].split(",");
const col = (name) => header.indexOf(name);
const [cc, cd, kc, kd, wc, wd, rv] = [
	"county_code",
	"county_display",
	"constituency_code",
	"constituency_display",
	"ward_code",
	"ward_display",
	"registered_voters_2022",
].map(col);

// The seed has no quoted fields (checked here, so a future one fails loudly).
const counties = new Map();
const constituencies = new Map();
const wards = [];
for (const line of csv.slice(1)) {
	if (line.includes('"')) throw new Error(`quoted CSV field not supported: ${line}`);
	const r = line.split(",");
	const county = Number(r[cc]);
	const constituency = Number(r[kc]);
	counties.set(county, r[cd]);
	constituencies.set(constituency, [r[kd], county]);
	wards.push([Number(r[wc]), r[wd], constituency, Number(r[rv])]);
}

const out = {
	version: "iebc-2022",
	// [code, name]
	counties: [...counties].map(([code, name]) => [code, name]),
	// [code, name, county code]
	constituencies: [...constituencies].map(([code, [name, county]]) => [code, name, county]),
	// [code, name, constituency code, registered voters]
	wards,
};
writeFileSync(resolve(here, "../src/data/iebc-2022.json"), `${JSON.stringify(out)}\n`);
console.log(
	`iebc-2022: ${out.counties.length} counties, ${out.constituencies.length} constituencies, ${wards.length} wards`,
);
