import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Skiff",
    short_name: "Skiff",
    description: "Ship it to a server you own.",
    start_url: "/",
    display: "standalone",
    background_color: "#142c29",
    theme_color: "#142c29",
    icons: [{ src: "/icon.svg", type: "image/svg+xml", sizes: "any" }],
  };
}
