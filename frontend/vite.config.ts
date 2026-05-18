import path from "path"
import fs from "fs"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vite"

function adminDevRoute(): Plugin {
  return {
    name: "admin-dev-route",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = req.url?.split("?")[0]
        if (url !== "/admin" && url !== "/admin/" && url !== "/admin/index.html") {
          next()
          return
        }

        const adminIndex = path.resolve(__dirname, "public/admin/index.html")
        fs.readFile(adminIndex, "utf8", (err, html) => {
          if (err) {
            next(err)
            return
          }
          res.statusCode = 200
          res.setHeader("Content-Type", "text/html; charset=utf-8")
          res.end(html)
        })
      })
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [adminDevRoute(), react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY_TARGET || "http://localhost:8080",
        changeOrigin: true,
      },
      "/wasm": {
        target: process.env.VITE_API_PROXY_TARGET || "http://localhost:8080",
        changeOrigin: true,
      },
      "^/[^/]+/.+\\.wasm(?:\\?.*)?$": {
        target: process.env.VITE_API_PROXY_TARGET || "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
})
