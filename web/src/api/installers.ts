import client from './client';

export interface Installer {
  name: string;
  os: string;
  arch: string;
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
