import client from './client';

export interface Installer {
  name: string;
  os: string;
  arch: string;
  kind: string;
  size: number;
  sha256: string;
  url: string;
}

export async function fetchInstallers(): Promise<Installer[]> {
  const res = await client.get('/v2/installers');
  return res.data.installers || [];
}

// downloadInstaller fetches the installer file with the shared client (so
// the auth header is sent) and triggers a browser download under its name.
// The server's `url` field is already relative to the `/api` base the
// client uses, so it is used as-is.
export const downloadInstaller = async (installer: Installer) => {
  const path = installer.url;
  const { data } = await client.get(path, { responseType: 'blob' });
  const url = URL.createObjectURL(data as Blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = installer.name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
};

// origin returns the browser-visible server origin (scheme://host, no path)
// so install commands pasted on a target machine can reach this deployment.
export function serverOrigin(): string {
  return window.location.origin;
}

// shellInstallCommand builds the one-command install for a script-based
// installer: curl the script from this server and pipe it to bash with the
// server flag pointing back at this deployment. It runs under sudo (it
// writes a systemd unit) and passes --register, without which install.sh
// aborts for lack of a device key.
export function shellInstallCommand(origin: string): string {
  return `curl -sL ${origin}/api/v2/installers/install.sh | sudo bash -s -- --server ${origin} --register`;
}

// powershellInstallCommand builds the one-command install for Windows,
// downloading install.ps1 from this server and running it with -Register.
export function powershellInstallCommand(origin: string): string {
  return `irm ${origin}/api/v2/installers/install.ps1 -OutFile install.ps1; .\\install.ps1 -Server "${origin}" -Register`;
}
