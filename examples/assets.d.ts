declare module "*.md" {
  const contents: string;
  export default contents;
}

declare module "virtual:pwa-register" {
  export function registerSW(): string;
}
