import { test, expect } from "@playwright/test";

test("задержанная отправка сохраняет следующий черновик сообщения", async ({
  page,
}) => {
  let release;
  const responseReady = new Promise((resolve) => {
    release = resolve;
  });
  let started;
  const requestStarted = new Promise((resolve) => {
    started = resolve;
  });
  let sends = 0;
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/auth/refresh"))
      return route.fulfill({ json: { access_token: "test-token" } });
    if (path.endsWith("/me"))
      return route.fulfill({ json: { id: "buyer", name: "Покупатель" } });
    if (path.endsWith("/messages")) {
      if (route.request().method() === "POST") {
        sends++;
        expect(route.request().postDataJSON().body).toBe("Первое сообщение");
        started();
        await responseReady;
        return route.fulfill({ json: { id: "sent" } });
      }
      return route.fulfill({
        json: {
          conversation: { title: "Диалог" },
          items: [],
          next_cursor: "0",
        },
      });
    }
    return route.continue();
  });
  await page.goto("/#chat/test-conversation");
  const draft = page.getByLabel("Сообщение", { exact: true });
  const send = page.getByRole("button", { name: "Отправить", exact: true });
  await draft.fill("Первое сообщение");
  await send.click();
  await requestStarted;
  await expect(send).toBeDisabled();
  await draft.fill("Следующий черновик");
  await draft.evaluate((input) => input.form.requestSubmit());
  release();
  await expect(send).toBeEnabled();
  await expect(draft).toHaveValue("Следующий черновик");
  expect(sends).toBe(1);
});

test("параллельные 401 используют один refresh, включая запоздавший ответ", async ({
  page,
}) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Свежие объявления" }),
  ).toBeVisible();
  const refreshCount = await page.evaluate(async () => {
    const api = await import("/api.js");
    const original = window.fetch;
    let refreshes = 0;
    window.fetch = async (url, options) => {
      if (url.endsWith("/auth/refresh")) {
        refreshes++;
        await new Promise((resolve) => setTimeout(resolve, 30));
        return Response.json({ access_token: "fresh-token" });
      }
      if (url.includes("/probe")) {
        if (options.headers.get("Authorization") === "Bearer fresh-token")
          return Response.json({ ok: true });
        await new Promise((resolve) =>
          setTimeout(resolve, url.endsWith("2") ? 80 : 0),
        );
        return Response.json(
          { error: { message: "expired" } },
          { status: 401 },
        );
      }
      return original(url, options);
    };
    try {
      await Promise.all([api.request("/probe1"), api.request("/probe2")]);
      return refreshes;
    } finally {
      window.fetch = original;
    }
  });
  expect(refreshCount).toBe(1);
});
test("публикация, безопасный текст и сохранение формы при ошибке", async ({
  page,
  browser,
}) => {
  const unique = String(Date.now());
  const listingTitle = "<img src=x onerror=alert(1)> " + unique;
  await page.goto("/");
  await page.getByRole("link", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Создать аккаунт" }).click();
  await page.getByLabel("Имя").fill("Анна");
  await page.getByLabel("Email").fill(`browser-${Date.now()}@example.com`);
  await page.getByLabel("Пароль").fill("strong-password");
  await page.getByRole("button", { name: "Зарегистрироваться" }).click();
  await expect(
    page.getByRole("link", { name: "Анна", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Подать объявление" }).click();
  await page.getByLabel("Название").fill(listingTitle);
  await page
    .getByLabel("Описание")
    .fill("Горный велосипед в хорошем состоянии");
  await page.getByLabel("Цена, ₽").fill("15000");
  await page.getByLabel("Категория").selectOption("sport");
  await page.getByRole("button", { name: "Сохранить черновик" }).click();
  await expect(page.getByText("Изменения сохранены")).toBeVisible();
  await page.getByLabel("Добавить фотографию").setInputFiles({
    name: "photo.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGNQKw0FIgYIBQAZkgPBSf4NCgAAAABJRU5ErkJggg==",
      "base64",
    ),
  });
  await expect(page.getByAltText("Фото объявления")).toBeVisible();
  await expect
    .poll(() =>
      page
        .getByAltText("Фото объявления")
        .evaluate((img) => img.complete && img.naturalWidth > 0),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await expect(page.getByText("Опубликовано", { exact: true })).toBeVisible();
  const listingID = page.url().split("#edit/")[1];
  await page.route("**/api/v1/listings/*", async (route) => {
    if (route.request().method() === "PATCH")
      return route.fulfill({
        status: 500,
        contentType: "application/json",
        body: '{"error":{"message":"Тестовая ошибка"}}',
      });
    return route.continue();
  });
  await page.getByLabel("Название").fill("Текст не должен потеряться");
  await page.getByRole("button", { name: "Сохранить изменения" }).click();
  await expect(page.getByLabel("Название")).toHaveValue(
    "Текст не должен потеряться",
  );
  await expect(page.getByText("Тестовая ошибка")).toBeVisible();
  await page.unroute("**/api/v1/listings/*");

  const buyerContext = await browser.newContext({
    baseURL: process.env.BASE_URL || "http://127.0.0.1:8080",
  });
  const buyer = await buyerContext.newPage();
  try {
    await buyer.goto("/#register");
    await buyer.getByLabel("Имя").fill("Борис");
    await buyer.getByLabel("Email").fill(`buyer-browser-${unique}@example.com`);
    await buyer.getByLabel("Пароль").fill("strong-password");
    await buyer.getByRole("button", { name: "Зарегистрироваться" }).click();
    await expect(
      buyer.getByRole("link", { name: "Борис", exact: true }),
    ).toBeVisible();
    await buyer.getByLabel("Что ищете?").fill(unique);
    await expect(async () => {
      await buyer.getByRole("button", { name: "Найти", exact: true }).click();
      await expect(
        buyer.getByRole("heading", { name: listingTitle, exact: true }),
      ).toBeVisible();
    }).toPass({ timeout: 20000 });
    await buyer
      .getByRole("heading", { name: listingTitle, exact: true })
      .click();
    await expect(
      buyer.getByRole("heading", { name: listingTitle, exact: true }),
    ).toBeVisible();
    await expect(buyer.locator('img[src="x"]')).toHaveCount(0);
    await buyer.getByRole("button", { name: "Написать продавцу" }).click();
    await buyer
      .getByLabel("Сообщение", { exact: true })
      .fill("Здравствуйте! Ещё продаётся?");
    await buyer.getByRole("button", { name: "Отправить" }).click();
    await expect(
      buyer.getByText("Здравствуйте! Ещё продаётся?", { exact: true }),
    ).toBeVisible();
    const chatHash = new URL(buyer.url()).hash;
    await page.goto("/" + chatHash);
    await expect(
      page.getByText("Здравствуйте! Ещё продаётся?", { exact: true }),
    ).toBeVisible();
    await page
      .getByLabel("Сообщение", { exact: true })
      .fill("Да, можно забрать завтра.");
    await page.getByRole("button", { name: "Отправить" }).click();
    await expect(
      buyer.getByText("Да, можно забрать завтра.", { exact: true }),
    ).toBeVisible({ timeout: 12000 });
    await page.screenshot({
      path: "../../.local/chat-desktop.png",
      fullPage: true,
    });
    await page.goto("/#edit/" + listingID);
    await page.getByRole("button", { name: "Снять с продажи" }).click();
    await expect(
      page.getByText("Снято с продажи", { exact: true }),
    ).toBeVisible();
    await buyer.getByRole("link", { name: "Каталог", exact: true }).click();
    await buyer.getByLabel("Что ищете?").fill(unique);
    await expect(async () => {
      await buyer.getByRole("button", { name: "Найти", exact: true }).click();
      await expect(
        buyer.getByText("Пока ничего не найдено.", { exact: false }),
      ).toBeVisible();
    }).toPass({ timeout: 20000 });
  } finally {
    await buyerContext.close();
  }
});
