import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'wxt';

export default defineConfig({
  modules: ['@wxt-dev/module-react', '@wxt-dev/auto-icons'],
  vite: () => ({
    plugins: [tailwindcss()],
  }),
  manifest: {
    name: 'Knot',
    description: '嗅探网页资源，一键交给 Knot 下载',
    permissions: ['webRequest', 'storage', 'cookies', 'contextMenus', 'notifications'],
    host_permissions: ['<all_urls>'],
    // 公钥（SPKI）：让「Load unpacked 开发态」与 crx 安装态共用同一扩展 ID
    // （cmakgnanekacdphdbnkojhbgehiodlal），与 extension-key.pem 配对。
    key: 'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAw7S+R66lpjs2ksCE755g+MMlHc5okqS5bl9d/Cr/4hHFygNf5/HsJBAzeFaE/A5I2DQwsEGTvpDQp9837UWGcadMvseyEyUv+JkOkgdLc6WoH5kxTRo430hOzakGKIa9Ueh9ApMoFEW2JWpnBkiYNi/QfcQTGyCbg9Qc79x2DtnYo8OyF5k5r2QlefnfaGlZ40gMrA+T0/fV7/XhT6Wy4y74E+xSi0G/A/dD17/IGPZP+Ds0jXZDqSjKvSEc30uDU6EKRovSUKpQYSwYdiYvHx73LnTPkxFTVv9yKjUEM5F4S/6m7xGtt9AZrmeVONKtZqHOOUa1Tgc1jEQUQiJ5/QIDAQAB',
  },
});
