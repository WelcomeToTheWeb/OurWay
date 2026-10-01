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
// The server's `url` field is a full path (e.g. /api/v2/installers/x.exe);
// the client already uses `/api` as its base URL, so that prefix is
// stripped to avoid a doubled `/api/api` path.
export const downloadInstaller = async (installer: Installer) => {
  const path = installer.url.replace(/^\/api(?=\/)/, '');
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
