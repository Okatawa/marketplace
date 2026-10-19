import { defineConfig } from '@playwright/test';
export default defineConfig({testDir:'.',testMatch:'*.spec.js',workers:1,timeout:90000,use:{baseURL:process.env.BASE_URL||'http://127.0.0.1:8080',headless:true},reporter:'list'});
