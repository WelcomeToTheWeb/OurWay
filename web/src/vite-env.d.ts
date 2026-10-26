/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** The release stamp baked in by the build (docs/versioning.md). */
  readonly VITE_APP_VERSION?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
