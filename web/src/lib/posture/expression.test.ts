import { describe, expect, it } from "vitest";

import { isBooleanType, knownAttributes } from "~/lib/posture/attributes.ts";
import { checkExpression } from "~/lib/posture/check.ts";
import { errors, parseExpression, prefixes } from "~/lib/posture/expression.ts";

describe("posture expressions", () => {
  it("includes integration prefixes", () => {
    expect(prefixes).toStrictEqual([
      "node",
      "custom",
      "ip",
      "falcon",
      "sentinelOne",
      "intune",
      "jamfPro",
      "kandji",
      "kolide",
    ]);
  });

  it("parses valid integration expressions", () => {
    const falcon = parseExpression("falcon:ztaScore >= 50");
    expect(falcon.ok).toBe(true);

    const intune = parseExpression("intune:complianceState == 'compliant'");
    expect(intune.ok).toBe(true);

    const sentinelOne = parseExpression("sentinelOne:infected == false");
    expect(sentinelOne.ok).toBe(true);
  });

  it("rejects unknown attribute prefixes with descriptive error", () => {
    const result = parseExpression("unknown:attr == 'test'");
    expect(result).toStrictEqual({
      ok: false,
      error: {
        message: `${errors.prefix}, got "unknown"`,
        from: 0,
        to: 7,
      },
    });
  });

  it("validates provider attributes in checkExpression without unknown attribute warnings", () => {
    const problems = checkExpression("falcon:ztaScore >= 50");
    expect(problems).toHaveLength(0);

    const boolProblems = checkExpression("sentinelOne:infected == true");
    expect(boolProblems).toHaveLength(0);

    const stringProblems = checkExpression("kolide:authState == 'Good'");
    expect(stringProblems).toHaveLength(0);
  });

  it("warns on boolean type mismatch for provider attributes", () => {
    const problems = checkExpression("kandji:mdmEnabled == 'yes'");
    expect(problems).toHaveLength(1);
    expect(problems[0]?.severity).toBe("warning");
    expect(problems[0]?.message).toContain("is true or false");
  });

  it("warns on unknown provider attributes", () => {
    const problems = checkExpression("falcon:unknownAttr == 1");
    expect(problems).toHaveLength(1);
    expect(problems[0]?.severity).toBe("warning");
    expect(problems[0]?.message).toContain("is not an attribute the server reports");
  });

  it("warns when a number is compared with a quoted value, whatever the operator", () => {
    const texts = [
      "falcon:ztaScore == '80'",
      "falcon:ztaScore != '80'",
      "falcon:ztaScore >= '80'",
      "falcon:ztaScore < '80'",
      "sentinelOne:activeThreats IN ['1']",
      "sentinelOne:activeThreats NOT IN ['1', '2']",
    ];

    for (const text of texts) {
      const problems = checkExpression(text);

      expect(problems.length).toBeGreaterThan(0);
      expect(problems[0]?.severity).toBe("warning");
      expect(problems[0]?.message).toContain("is a number, so a quoted value never matches");
    }
  });

  it("leaves a number compared with a number alone", () => {
    expect(checkExpression("falcon:ztaScore >= 80")).toStrictEqual([]);
    expect(checkExpression("sentinelOne:activeThreats IN [0, 1]")).toStrictEqual([]);
  });
});

describe(isBooleanType, () => {
  it("holds for every attribute that takes true or false, whichever integration reports it", () => {
    // The completions in language.ts ask this same question, and once knew only half the spelling.
    const booleans = knownAttributes
      .filter((attribute) => isBooleanType(attribute.type))
      .map((attribute) => attribute.name);

    expect(booleans).toContain("node:tsAutoUpdate");
    expect(booleans).toContain("intune:isEncrypted");
    expect(booleans).toContain("jamfPro:remoteManaged");
    expect(booleans).toContain("sentinelOne:infected");
  });

  it("holds for nothing else", () => {
    expect(isBooleanType("string")).toBe(false);
    expect(isBooleanType("number")).toBe(false);
    expect(isBooleanType("version")).toBe(false);
  });
});
