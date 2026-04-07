/// <reference types="vite/client" />

declare const __APP_VERSION__: string;

declare module 'cytoscape-fcose' {
  const fcose: cytoscape.Ext;
  export default fcose;
}
