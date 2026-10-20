// Optional PDF export, using the same Chromium installed for Mermaid CLI.
import puppeteer from 'puppeteer';
import { fileURLToPath, pathToFileURL } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const browser = await puppeteer.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.goto(pathToFileURL(path.join(root, 'dist/envoy-learning-book.html')).href, { waitUntil: 'networkidle0' });
  await page.evaluate(() => document.fonts.ready);
  await page.pdf({ path: path.join(root, 'dist/envoy-learning-book.pdf'), format: 'A4', printBackground: true,
    margin: { top: '18mm', right: '15mm', bottom: '18mm', left: '15mm' },
    displayHeaderFooter: true, headerTemplate: '<span></span>',
    footerTemplate: '<div style="font-size:9px;width:100%;text-align:center;color:#666"><span class="pageNumber"></span> / <span class="totalPages"></span></div>' });
  console.log('Exported dist/envoy-learning-book.pdf');
} finally { await browser.close(); }
