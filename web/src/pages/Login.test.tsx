import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { Login } from "./Login";
import { AuthProvider } from "../auth/context";

vi.mock("../api/auth", () => ({
  getAuthStatus: vi.fn(),
  login: vi.fn(),
  register: vi.fn(),
}));
vi.mock("../api/sso", () => ({
  listSSOProviders: vi.fn(),
  authorizeProvider: vi.fn((name: string) => `/api/auth/sso/${name}/authorize`),
  exchangeSSOCode: vi.fn(),
}));

import { getAuthStatus } from "../api/auth";
import { listSSOProviders } from "../api/sso";

const mockedStatus = vi.mocked(getAuthStatus);
const mockedList = vi.mocked(listSSOProviders);

function renderPage() {
  return render(
    <AuthProvider>
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    </AuthProvider>,
  );
}

describe("Login — the provider list (2.4-4)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    mockedStatus.mockResolvedValue({ has_users: true } as never);
  });

  it("offers the providers in the order the operator configured", async () => {
    // The server answers in `sort_order, name`; the login page must not re-sort it
    // into something the operator did not choose.
    mockedList.mockResolvedValue([
      { type: "oidc", name: "keycloak-corp" },
      { type: "oidc", name: "authentik-corp" },
      { type: "oidc", name: "google" },
    ]);
    renderPage();
    const buttons = await screen.findAllByRole("button", {
      name: /Sign in with/,
    });
    // Display names are display names (the page capitalizes a slug for reading);
    // the assertion here is the order, so compare case-insensitively.
    const labels = buttons.map((b) => (b.textContent ?? "").toLowerCase());
    expect(labels[0]).toContain("keycloak-corp");
    expect(labels[1]).toContain("authentik-corp");
    expect(labels[2]).toContain("google");
  });

  it("offers a SAML provider by the name the operator chose, through the same door", async () => {
    // 2.4-7: the login page is protocol-blind. A SAML entity ID (`urn:ourway:...`)
    // is a machine identifier a person cannot check, so the server answers with the
    // provider name as the label, and the button is the same authorize route.
    mockedList.mockResolvedValue([
      {
        type: "oidc",
        protocol: "oidc",
        name: "keycloak-corp",
        label: "https://idp.corp/realms/ourway",
      },
      { type: "saml", protocol: "saml", name: "idp-door", label: "idp-door" },
    ]);
    renderPage();
    const buttons = await screen.findAllByRole("button", {
      name: /Sign in with/,
    });
    const labels = buttons.map((b) => (b.textContent ?? "").toLowerCase());
    expect(labels[1]).toContain("idp-door");
    expect(labels[1]).not.toContain("urn:");
    // The OIDC door keeps showing its issuer, which is the place a person recognises.
    expect(labels[0]).toContain("https://idp.corp/realms/ourway");
  });

  it("shows only what is enabled, and says nothing when there is none", async () => {
    mockedList.mockResolvedValue([]);
    renderPage();
    await waitFor(() => expect(mockedList).toHaveBeenCalled());
    expect(
      screen.queryAllByRole("button", { name: /Sign in with/ }),
    ).toHaveLength(0);
  });
});
