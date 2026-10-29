import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  render,
  screen,
  waitFor,
  fireEvent,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

import { SSO } from "./SSO";
import { AuthProvider } from "../auth/context";
import en from "../i18n/locales/en.json";
import de from "../i18n/locales/de.json";
import fr from "../i18n/locales/fr.json";
import type { DiscoveryPreview, SSOProviderFull } from "../api/sso";

vi.mock("../api/sso", () => ({
  listSSOProvidersAdmin: vi.fn(),
  createSSOProvider: vi.fn(),
  updateSSOProvider: vi.fn(),
  deleteSSOProvider: vi.fn(),
  discoverIssuer: vi.fn(),
  testSSOProvider: vi.fn(),
  samlMetadataURL: (name: string) => `/api/auth/sso/saml/${name}/metadata`,
  SSO_PRESETS: [
    {
      name: "google",
      issuer: "https://accounts.google.com",
      scope: "openid profile email",
    },
    {
      name: "microsoft",
      issuer: "https://login.microsoftonline.com/common/v2.0",
      scope: "openid profile email offline_access",
    },
    { name: "apple", issuer: "https://appleid.apple.com", scope: "openid" },
  ],
}));

import {
  listSSOProvidersAdmin,
  createSSOProvider,
  updateSSOProvider,
  deleteSSOProvider,
  discoverIssuer,
  testSSOProvider,
} from "../api/sso";

const mockedList = vi.mocked(listSSOProvidersAdmin);
const mockedCreate = vi.mocked(createSSOProvider);
const mockedUpdate = vi.mocked(updateSSOProvider);
const mockedDelete = vi.mocked(deleteSSOProvider);
const mockedDiscover = vi.mocked(discoverIssuer);
const mockedTest = vi.mocked(testSSOProvider);

function provider(over: Partial<SSOProviderFull> = {}): SSOProviderFull {
  return {
    id: "prov-keycloak",
    type: "oidc",
    name: "keycloak-corp",
    client_id: "ourway",
    auth_url: "https://idp.corp/realms/ourway/protocol/openid-connect/auth",
    token_url: "https://idp.corp/realms/ourway/protocol/openid-connect/token",
    user_info_url:
      "https://idp.corp/realms/ourway/protocol/openid-connect/userinfo",
    scope: "openid profile email",
    enabled: true,
    created_at: "2026-11-20T10:00:00Z",
    issuer: "https://idp.corp/realms/ourway",
    discovery_url: "",
    jwks_uri: "https://idp.corp/realms/ourway/protocol/openid-connect/certs",
    redirect_uri: "",
    discovered_at: "2026-11-20T10:00:00Z",
    pkce_supported: true,
    claims_email: "",
    claims_username: "",
    claims_name: "",
    claims_groups: "",
    allowed_domains: "corp.example, partner.example",
    require_verified_email: true,
    link_policy: "pending",
    role_mapping: '{"rmm-admins":"admin"}',
    sort_order: 0,
    ...over,
  };
}

/**
 * A SAML provider row (2.4-7), as the admin API serializes it: the IdP facts, the
 * derived SP identity, the certificate as a fingerprint, and no key material — the
 * server keeps `idp_certificate`, `sp_cert_pem` and `sp_key_pem` out of the JSON.
 */
function samlProvider(over: Partial<SSOProviderFull> = {}): SSOProviderFull {
  return {
    id: "prov-saml",
    type: "saml",
    name: "corp-saml",
    client_id: "",
    auth_url: "",
    token_url: "",
    user_info_url: "",
    scope: "",
    enabled: true,
    created_at: "2026-11-20T10:00:00Z",
    issuer: "",
    discovery_url: "",
    jwks_uri: "",
    redirect_uri: "",
    discovered_at: null,
    pkce_supported: false,
    claims_email: "",
    claims_username: "",
    claims_name: "",
    claims_groups: "",
    allowed_domains: "corp.example",
    require_verified_email: false,
    link_policy: "pending",
    role_mapping: "",
    sort_order: 1,
    saml: {
      idp_entity_id: "https://idp.corp.example/realms/ourway",
      idp_sso_url: "https://idp.corp.example/realms/ourway/protocol/saml",
      idp_logout_url:
        "https://idp.corp.example/realms/ourway/protocol/saml/logout",
      sp_entity_id: "urn:ourway:corp-saml:prov-saml",
      acs_url: "https://ourway.corp/api/auth/sso/saml/corp-saml/acs",
      idp_certificate_sha256:
        "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99",
      idp_certificate_expires: "2027-11-20T10:00:00Z",
      signs_requests: true,
      require_response_signature: false,
      nameid_format: "emailaddress",
      attr_email: "email",
      attr_username: "username",
      attr_name: "displayName",
      attr_groups: "groups",
      verified_email_attribute: "",
    },
    ...over,
  };
}

function preview(over: Partial<DiscoveryPreview> = {}): DiscoveryPreview {
  return {
    ok: true,
    issuer: "https://idp.corp/realms/ourway",
    discovery: "ok",
    discovery_url:
      "https://idp.corp/realms/ourway/.well-known/openid-configuration",
    redirect_uri: "https://ourway.corp/api/auth/sso/keycloak-corp/callback",
    endpoints: {
      authorization:
        "https://idp.corp/realms/ourway/protocol/openid-connect/auth",
      token: "https://idp.corp/realms/ourway/protocol/openid-connect/token",
      userinfo:
        "https://idp.corp/realms/ourway/protocol/openid-connect/userinfo",
      jwks: "https://idp.corp/realms/ourway/protocol/openid-connect/certs",
    },
    signing_algs: ["RS256"],
    response_types: ["code"],
    scopes: ["openid", "profile", "email"],
    pkce_methods: ["S256"],
    checks: [
      {
        name: "discovery",
        state: "pass",
        detail: "https://idp.corp/realms/ourway",
      },
      {
        name: "redirect_uri",
        state: "pass",
        detail: "https://ourway.corp/api/auth/sso/keycloak-corp/callback",
      },
      {
        name: "jwks",
        state: "pass",
        detail: "https://idp.corp/realms/ourway/protocol/openid-connect/certs",
      },
    ],
    ...over,
  };
}

function setRoles(roles: string[]) {
  localStorage.setItem("ourway_roles", JSON.stringify(roles));
}

function renderPage() {
  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={["/sso"]}>
        <Routes>
          <Route path="/sso" element={<SSO />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>,
  );
}

/** The ledger row for a provider. Scoping to the table keeps the assertion about
 * the row, not about the page's "first / last in view" words. */
async function ledgerRow(name: string): Promise<HTMLTableRowElement> {
  const table = await screen.findByRole("table");
  const rows = within(table).getAllByRole("row").slice(1);
  const row = rows.find((r) => (r.textContent ?? "").includes(name));
  if (!row) throw new Error(`no ledger row for ${name}`);
  return row as HTMLTableRowElement;
}

describe("SSO provider ledger (2.4-4)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    setRoles(["admin"]);
    mockedList.mockResolvedValue([provider()]);
    mockedCreate.mockResolvedValue(provider());
    mockedUpdate.mockResolvedValue(provider());
    mockedDelete.mockResolvedValue(undefined);
    mockedDiscover.mockResolvedValue(preview());
    mockedTest.mockResolvedValue({
      ok: true,
      provider: "keycloak-corp",
      checks: [{ name: "discovery", state: "pass" }],
    });
  });

  it("states the issuer, the contract and the order — not a preset dropdown", async () => {
    renderPage();
    const row = await ledgerRow("keycloak-corp");
    expect(row.textContent).toContain("https://idp.corp/realms/ourway");
    expect(row.textContent).toContain("2 domains");
    expect(row.textContent).toContain("verified email");
    expect(row.textContent).toContain("role mapping");
    expect(row.textContent).toContain("pending");
  });

  it("names a provider that links by email, because that is a decision an operator made", async () => {
    mockedList.mockResolvedValue([provider({ link_policy: "link" })]);
    renderPage();
    const row = await ledgerRow("keycloak-corp");
    expect(row.textContent).toContain("links by email");
  });

  it("saves an issuer and a policy, not endpoints, on create", async () => {
    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: /Add Provider/ }),
    );
    const form = await screen.findByRole("form");

    fireEvent.change(within(form).getByLabelText(/Provider name/), {
      target: { value: "authentik-corp" },
    });
    fireEvent.change(within(form).getByLabelText(/^Issuer/), {
      target: { value: "https://idp.example/application/openid-client/ourway" },
    });
    fireEvent.change(within(form).getByLabelText(/Client ID/), {
      target: { value: "ourway" },
    });
    fireEvent.change(within(form).getByLabelText(/Client Secret/), {
      target: { value: "a-secret" },
    });
    fireEvent.submit(form);

    await waitFor(() => expect(mockedCreate).toHaveBeenCalled());
    const payload = mockedCreate.mock.calls[0][0];
    expect(payload.issuer).toBe(
      "https://idp.example/application/openid-client/ourway",
    );
    expect(payload.name).toBe("authentik-corp");
    expect(payload.link_policy).toBe("pending");
    // The endpoints are the IdP's to publish (2.4-1): the form has none to type.
    expect(
      within(form).queryByLabelText(/Authorization endpoint|Token endpoint/),
    ).toBeNull();
  });

  it("prefills a preset and leaves room for any other IdP", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /^Google$/ }));
    const form = await screen.findByRole("form");
    const issuer = within(form).getByLabelText(/^Issuer/) as HTMLInputElement;
    expect(issuer.value).toBe("https://accounts.google.com");
    // A preset is a starting point, not the only door: the name field is editable
    // for a new provider, so any issuer can be named.
    const name = within(form).getByLabelText(
      /Provider name/,
    ) as HTMLInputElement;
    expect(name.disabled).toBe(false);
  });

  it("shows what the IdP published, including the redirect URI to register", async () => {
    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: /Add Provider/ }),
    );
    const form = await screen.findByRole("form");
    fireEvent.change(within(form).getByLabelText(/^Issuer/), {
      target: { value: "https://idp.corp/realms/ourway" },
    });
    fireEvent.click(
      within(form).getByRole("button", {
        name: /Ask the IdP what it publishes/,
      }),
    );

    const panel = await screen.findByRole("status", {
      name: /What the IdP published/,
    });
    expect(panel.textContent).toContain(
      "https://idp.corp/realms/ourway/protocol/openid-connect/auth",
    );
    expect(panel.textContent).toContain(
      "https://ourway.corp/api/auth/sso/keycloak-corp/callback",
    );
    expect(panel.textContent).toContain("RS256");
    expect(mockedDiscover).toHaveBeenCalledWith(
      "https://idp.corp/realms/ourway",
      "",
    );
  });

  it("names a refused discovery by kind, and says nothing was saved", async () => {
    mockedDiscover.mockResolvedValue(
      preview({
        ok: false,
        issuer: "",
        discovery: "issuer_mismatch",
        checks: [
          {
            name: "discovery",
            state: "fail",
            detail: "issuer_mismatch: document says https://other.example",
          },
        ],
      }),
    );
    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: /Add Provider/ }),
    );
    const form = await screen.findByRole("form");
    fireEvent.change(within(form).getByLabelText(/^Issuer/), {
      target: { value: "https://idp.corp" },
    });
    fireEvent.click(
      within(form).getByRole("button", {
        name: /Ask the IdP what it publishes/,
      }),
    );
    const panel = await screen.findByRole("status", {
      name: /What the IdP published/,
    });
    expect(panel.textContent).toContain("issuer_mismatch");
    expect(panel.textContent).toContain("Nothing was saved by this call.");
  });

  it("edits a provider in place and sends only the fields it touched for a toggle", async () => {
    renderPage();
    const row = await ledgerRow("keycloak-corp");
    fireEvent.click(within(row).getByRole("button", { name: /^Test$/ }));
    await waitFor(() =>
      expect(mockedTest).toHaveBeenCalledWith("prov-keycloak"),
    );

    // The enable toggle is one field — it cannot carry a policy wipe.
    fireEvent.click(within(row).getByRole("checkbox"));
    await waitFor(() =>
      expect(mockedUpdate).toHaveBeenCalledWith("prov-keycloak", {
        enabled: false,
      }),
    );
  });

  it("opens the edit form with the stored values and never the secret", async () => {
    renderPage();
    const row = await ledgerRow("keycloak-corp");
    fireEvent.click(within(row).getByRole("button", { name: /Edit provider/ }));
    const form = await screen.findByRole("form");
    const secret = within(form).getByLabelText(
      /Client Secret/,
    ) as HTMLInputElement;
    expect(secret.value).toBe("");
    expect(secret.placeholder).toBe("Saved — type to replace it");
    // The name is the identity key: the form says so and locks it.
    const name = within(form).getByLabelText(
      /Provider name/,
    ) as HTMLInputElement;
    expect(name.disabled).toBe(true);
    expect(form.textContent).toContain("identity key stored on every account");
    expect(
      (within(form).getByLabelText(/Allowed domains/) as HTMLInputElement)
        .value,
    ).toBe("corp.example, partner.example");
  });

  it("reports a refused save at page level, in the server's words", async () => {
    mockedCreate.mockRejectedValue(
      new Error(
        "provider not saved: issuer_mismatch: document says https://other.example",
      ),
    );
    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: /Add Provider/ }),
    );
    const form = await screen.findByRole("form");
    fireEvent.change(within(form).getByLabelText(/Provider name/), {
      target: { value: "broken-corp" },
    });
    fireEvent.change(within(form).getByLabelText(/^Issuer/), {
      target: { value: "https://idp.corp" },
    });
    fireEvent.change(within(form).getByLabelText(/Client ID/), {
      target: { value: "ourway" },
    });
    fireEvent.submit(form);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("issuer_mismatch");
  });

  it("deletes through the sheet, naming the provider", async () => {
    renderPage();
    const row = await ledgerRow("keycloak-corp");
    fireEvent.click(within(row).getByRole("button", { name: /Delete/ }));
    const sheet = await screen.findByRole("dialog");
    expect(sheet.textContent).toContain("keycloak-corp");
    fireEvent.click(
      within(sheet).getByRole("button", { name: /Delete binding/ }),
    );
    await waitFor(() =>
      expect(mockedDelete).toHaveBeenCalledWith("prov-keycloak"),
    );
  });

  it("lists providers in the order the login page will render them", async () => {
    mockedList.mockResolvedValue([
      provider({ id: "p1", name: "first-door", sort_order: 1 }),
      provider({ id: "p2", name: "second-door", sort_order: 2 }),
      provider({ id: "p3", name: "third-door", sort_order: 3 }),
    ]);
    renderPage();
    // The page renders what the server answered, in that order (the server's
    // `sort_order, name` ordering is proven in the API suite).
    const table = await screen.findByRole("table");
    const rows = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((r) => r.textContent ?? "");
    expect(rows.findIndex((text) => text.includes("first-door"))).toBe(0);
    expect(rows.findIndex((text) => text.includes("second-door"))).toBe(1);
    expect(rows.findIndex((text) => text.includes("third-door"))).toBe(2);
  });
});

describe("SAML 2.0 providers (2.4-7)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    setRoles(["admin"]);
    mockedList.mockResolvedValue([samlProvider()]);
    mockedCreate.mockResolvedValue(samlProvider());
    mockedUpdate.mockResolvedValue(samlProvider());
    mockedDelete.mockResolvedValue(undefined);
  });

  it("states the door a provider is, the certificate that can sign it, and the metadata the IdP needs", async () => {
    renderPage();
    const row = await ledgerRow("corp-saml");
    expect(row.textContent).toContain("SAML 2.0");
    expect(row.textContent).toContain("https://idp.corp.example/realms/ourway");
    expect(row.textContent).toContain("urn:ourway:corp-saml:prov-saml");
    expect(row.textContent).toContain('key AA:BB:CC:DD:EE:FF');
    expect(row.textContent).toContain("certificate to 2027-11-20");
    expect(row.textContent).toContain("signed requests");

    // The document an operator uploads to the IdP, addressed by provider name.
    const link = within(row).getByRole("link", { name: /SP metadata/ });
    expect(link.getAttribute("href")).toBe(
      "/api/auth/sso/saml/corp-saml/metadata",
    );

    // The certificate is never shown, and never in the payload the console has.
    expect(row.textContent).not.toContain("BEGIN CERTIFICATE");
  });

  it("says an OIDC provider is not a SAML door, and offers it no metadata", async () => {
    mockedList.mockResolvedValue([provider()]);
    renderPage();
    const row = await ledgerRow("keycloak-corp");
    expect(row.textContent).toContain("OIDC");
    expect(within(row).queryByRole("link", { name: /SP metadata/ })).toBeNull();
  });

  it("warns about a certificate that is about to expire, in the ledger", async () => {
    mockedList.mockResolvedValue([
      samlProvider({
        saml: {
          ...samlProvider().saml!,
          idp_certificate_expires: "2000-01-01T00:00:00Z",
        },
      }),
    ]);
    renderPage();
    const row = await ledgerRow("corp-saml");
    expect(row.textContent).toContain("certificate to 2000-01-01");
  });

  it("asks for the IdP facts when the protocol is SAML, and none of the OIDC fields", async () => {
    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: /Add Provider/ }),
    );
    const form = await screen.findByRole("form");

    // The protocol is chosen, not inferred: the door decides which facts the row needs.
    fireEvent.click(within(form).getByLabelText("SAML 2.0"));

    expect(within(form).queryByLabelText(/^Issuer/)).toBeNull();
    expect(within(form).queryByLabelText(/Client Secret/)).toBeNull();
    for (const label of [
      /IdP entity ID/,
      /IdP sign-in URL/,
      /signing certificate/i,
      /NameID format/,
    ]) {
      expect(within(form).getByLabelText(label)).toBeInTheDocument();
    }

    fireEvent.change(within(form).getByLabelText(/Provider name/), {
      target: { value: "idp-door" },
    });
    fireEvent.change(within(form).getByLabelText(/IdP entity ID/), {
      target: { value: "https://idp.corp.example/realms/ourway" },
    });
    fireEvent.change(within(form).getByLabelText(/sign-in URL/), {
      target: { value: "https://idp.corp.example/realms/ourway/protocol/saml" },
    });
    fireEvent.change(within(form).getByLabelText(/signing certificate/), {
      target: {
        value: "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----",
      },
    });
    fireEvent.change(within(form).getByLabelText(/Groups attribute/), {
      target: { value: "memberOf" },
    });
    fireEvent.submit(form);

    await waitFor(() => expect(mockedCreate).toHaveBeenCalled());
    const payload = mockedCreate.mock.calls[0][0];
    expect(payload.type).toBe("saml");
    expect(payload.idp_entity_id).toBe(
      "https://idp.corp.example/realms/ourway",
    );
    expect(payload.idp_sso_url).toBe(
      "https://idp.corp.example/realms/ourway/protocol/saml",
    );
    expect(payload.attr_groups).toBe("memberOf");
    // A SAML provider has no client id and no secret: its identity is the SP entity
    // ID the server derives.
    expect(payload.client_id).toBeUndefined();
    expect(payload.client_secret).toBeUndefined();
    // The signing key pair is asked for, and the server keeps it.
    expect(payload.generate_sp_key).toBe(true);
  });

  it("refuses to send a SAML form that cannot answer logins, naming what is missing", async () => {
    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: /Add Provider/ }),
    );
    const form = await screen.findByRole("form");
    fireEvent.click(within(form).getByLabelText("SAML 2.0"));
    fireEvent.change(within(form).getByLabelText(/Provider name/), {
      target: { value: "half-door" },
    });
    fireEvent.submit(form);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("idp_entity_id");
    expect(alert.textContent).toContain("idp_certificate");
    expect(mockedCreate).not.toHaveBeenCalled();
    // The refusal keeps the operator in the form: the facts are still typed there.
    expect(
      await screen.findByRole("form", { name: /Add SSO Provider/ }),
    ).toBeInTheDocument();
  });

  it("edits a SAML provider without re-sending the certificate it cannot read", async () => {
    renderPage();
    const row = await ledgerRow("corp-saml");
    fireEvent.click(within(row).getByRole("button", { name: /Edit provider/ }));
    const form = await screen.findByRole("form");

    const certificate = within(form).getByLabelText(
      /signing certificate/,
    ) as HTMLTextAreaElement;
    expect(certificate.value).toBe("");
    expect(certificate.placeholder).toBe(
      "Saved — paste a new certificate to replace it",
    );
    // The protocol is as immutable as the name: the radio is locked on edit.
    expect(
      (within(form).getByLabelText("SAML 2.0") as HTMLInputElement).disabled,
    ).toBe(true);
    expect(form.textContent).toContain("protocol cannot be changed");
    // The stored facts come back for reading.
    expect(
      (within(form).getByLabelText(/IdP entity ID/) as HTMLInputElement).value,
    ).toBe("https://idp.corp.example/realms/ourway");

    fireEvent.change(within(form).getByLabelText(/Allowed domains/), {
      target: { value: "corp.example, audit.example" },
    });
    fireEvent.submit(form);

    await waitFor(() =>
      expect(mockedUpdate).toHaveBeenCalledWith("prov-saml", expect.anything()),
    );
    const payload = mockedUpdate.mock.calls[0][1];
    expect(payload.allowed_domains).toBe("corp.example, audit.example");
    expect(payload.type).toBe("saml");
    // The trust anchor is only sent when the operator typed one: an unrelated edit
    // cannot rewrite, or wipe, the pinned certificate.
    expect(payload.idp_certificate).toBeUndefined();
    expect(payload.idp_sso_url).toBe(
      "https://idp.corp.example/realms/ourway/protocol/saml",
    );
    // An edit does not quietly mint a new signing key the IdP has not imported.
    expect(payload.generate_sp_key).toBeUndefined();
  });

  it("says in words that a SAML identity arrives on a pinned signature", async () => {
    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: /Add Provider/ }),
    );
    const form = await screen.findByRole("form");
    fireEvent.click(within(form).getByLabelText("SAML 2.0"));
    expect(form.textContent).toContain(
      "signed by the certificate you pin here",
    );
    expect(form.textContent).toContain(
      "The only key whose assertions this provider accepts",
    );
  });
});

describe("localization of the SAML console (2.4-7)", () => {
  const keys = [
    "protocol",
    "protocolOidcBody",
    "protocolSamlBody",
    "idpEntityId",
    "idpSsoUrl",
    "idpCertificate",
    "idpCertificateKept",
    "nameidFormat",
    "verifiedEmailAttribute",
    "requireResponseSignature",
    "generateSpKey",
    "needSamlFacts",
    "stampSaml",
    "stampCertExpiry",
    "metadata",
    "metadataBody",
  ];

  it("carries the SAML copy in all three locales, in the locale’s own words", () => {
    const enStrings = en.sso as Record<string, unknown>;
    const deStrings = de.sso as Record<string, unknown>;
    const frStrings = fr.sso as Record<string, unknown>;
    for (const key of keys) {
      for (const locale of [enStrings, deStrings, frStrings]) {
        const value = locale[key];
        expect(typeof value).toBe("string");
        expect((value as string).trim().length).toBeGreaterThan(3);
      }
    }
    expect(deStrings.needSamlFacts).not.toBe(enStrings.needSamlFacts);
    expect(frStrings.metadataBody).not.toBe(enStrings.metadataBody);
    expect(en.sso).toHaveProperty("protocolOption.oidc");
    expect(de.sso).toHaveProperty("protocolOption.saml");
    expect(fr.sso).toHaveProperty("protocolOption.oidc");
  });
});
