import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Knot",
  identifier: "com.besscroft.knot",
  version: "0.1.0",
  // `mygo dev` runs devCommand and loads devUrl; `mygo build` runs
  // buildCommand and embeds frontendDist into the app.
  devUrl: "http://localhost:5173",
  devCommand: "cd frontend && bun run dev",
  buildCommand: "cd frontend && bun run build",
  frontendDist: "frontend/dist",
    bindings: "frontend/src/mygo.ts",
    out: "build",
    // 应用内更新:manifest 与更新包都经官网 Worker(knot.zzzvoid.com)代理
    // GitHub Release,解决国内直连超时;私钥在 GitHub Secrets 的
    // MYGO_UPDATER_PRIVATE_KEY(本机 %AppConfigDir%\mygo\update-keys\)。
    updates: {
      publicKey: "UakPUCzR2ShcJsmYYBO3iCaL/T0Ka8HrMJeyRvkO9fw=",
      url: "https://knot.zzzvoid.com/update",
    },
});
