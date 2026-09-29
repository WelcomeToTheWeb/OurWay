# OurWay RMM 2.0: From Monitoring to Complete Remote Management

*Published: September 25, 2026*

We're thrilled to announce the release of **OurWay RMM 2.0** — a complete reimagining of what remote monitoring and management should be.

OurWay started as a lightweight monitoring tool focused on real-time visibility into your infrastructure. With 2.0, we've transformed it into a full-featured RMM platform that MSPs, IT teams, and DevOps engineers can rely on for day-to-day operations.

## What's New in 2.0

### See and Control Any Device — Instantly

Remote sessions have been on our roadmap for a while, and we're excited to finally ship them. With WebRTC-based screen sharing and control, you can view any managed device and take control — all from your browser.

No client installs. No plugins. Just click **Remote Session** on any device and you're in.

### Stop Chasing Updates Manually

Patch management was the #1 request from our community, and we listened. OurWay 2.0 now automates the entire update lifecycle:

1. **Scan** — Check all devices for missing updates
2. **Report** — See exactly what needs to be updated
3. **Approve** — Manually approve or set automatic policies
4. **Deploy** — Roll out updates with batching and scheduling
5. **Reboot** — Automatic reboot management
6. **Verify** — Get success/failure reports per device
7. **Rollback** — Roll back failed deployments via the API

Supports Windows Update, macOS Software Update, apt, yum, and more.

### Enterprise-Grade Security

We know security is non-negotiable. That's why 2.0 includes:

- **Role-Based Access Control**: Define who can see what, who can make changes, and who can administer the platform. Four built-in roles (Admin, Manager, Technician, Viewer) plus custom roles.
- **SSO Integration**: Log in with Google, Microsoft, or Apple. Just-in-time provisioning means new users get accounts automatically.
- **API v2**: Versioned endpoints for webhooks and API keys, plus rate limiting and scoped API keys.

### A Faster, More Beautiful Interface

OurWay 2.0 has a completely redesigned frontend:

- 🌙 **Dark Mode** — Easy on the eyes for 24/7 monitoring
- 📱 **Mobile-Friendly** — Manage your fleet from your phone
- ⌨️ **Keyboard Shortcuts** — Navigate like a power user
- 🌍 **Internationalization** — English, German, and French
- ♿ **Accessible** — WCAG 2.1 AA compliant

### Built to Scale

Whether you're managing 10 devices or 10,000, OurWay 2.0 handles it. We've implemented:

- Redis-backed rate limiting and caching
- Horizontal scaling support (multiple servers behind a load balancer)
- Time-series metrics storage with tiered retention
- WebSocket state sharing for real-time communication at scale

Our built-in scale load tests simulate 10,000+ concurrent devices.

## Getting Started

### Quick Install (Docker)

```bash
git clone https://github.com/ourway-rmm/ourway.git
cd ourway
docker compose up -d
open http://localhost:3000
```

### Install the Agent

```bash
curl -sL https://releases.ourway.io/agent/install.sh | bash -s \
  --server wss://ourway.example.com/ws \
  --key <device-key>
```

## Who Is OurWay For?

OurWay 2.0 is designed for:

- **MSPs** managing multiple clients' infrastructure
- **IT teams** in small to mid-sized businesses
- **DevOps engineers** managing server fleets
- **Sysadmins** who want enterprise features without enterprise pricing
- **Anyone** who believes technology should serve people, not the other way around

## What's Next?

OurWay 2.1 is already in the works, featuring:

- SAML 2.0 support
- GraphQL API
- Mobile app (iOS and Android)
- Enhanced automation with runbooks
- AI-powered alert correlation

## Try It Free

OurWay RMM is open source and free to use. Check it out:

- **GitHub:** https://github.com/ourway-rmm/ourway
- **Documentation:** https://docs.ourway.io/
- **Live Demo:** https://demo.ourway.io/

Thank you to our community for the incredible support, feedback, and contributions. OurWay is what it is because of you.

*Happy managing!*

— The OurWay Team

---

*OurWay RMM is available under the MIT License.*
