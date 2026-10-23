import { test, expect } from "@playwright/test";
test("account → import → summary → chapters → multi-turn evidence → refresh → delete", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("https://www.youtube-nocookie.com/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: "<!doctype html><title>Fixture iframe; real playback separately verified</title>",
    }),
  );
  await page.goto("/");
  await page.getByRole("button", { name: "第一次来？创建账号" }).click();
  const email = `browser-${Date.now()}@example.com`;
  await page.getByLabel("邮箱", { exact: true }).fill(email);
  await page.getByLabel("密码", { exact: true }).fill("browser-long-password");
  await page.getByRole("button", { name: "创建账号", exact: true }).click();
  await page
    .getByLabel("视频链接", { exact: true })
    .fill("https://youtu.be/dQw4w9WgXcQ");
  await page.getByRole("button", { name: "导入视频" }).click();
  await expect(page).toHaveURL(/\/media\/\d+$/);
  await expect(
    page.getByRole("heading", { name: "fixture video" }),
  ).toBeVisible();
  await expect(page.getByText("分析完成", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "核心观点", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".markdown script")).toHaveCount(0);
  await expect(page.locator(".markdown [onerror]")).toHaveCount(0);
  expect(
    await page.evaluate(() => Reflect.get(window, "__xss")),
  ).toBeUndefined();
  await page.getByRole("button", { name: /并发模型/ }).click();
  await expect(page.locator("iframe")).toHaveAttribute("src", /start=5/);
  await page.getByLabel("搜索字幕", { exact: false }).fill("goroutine");
  await expect(page.locator(".segment")).toHaveCount(1);
  await page.getByLabel("搜索字幕", { exact: false }).fill("");
  await page.getByLabel("关于这段视频，你想了解什么？").fill("如何实现并发？");
  await page.getByRole("button", { name: "提问 ↗", exact: true }).click();
  await expect(page.locator(".chat-message.assistant")).toHaveCount(1);
  await page.locator(".citations button").first().click();
  await expect(page.locator("iframe")).toHaveAttribute("src", /start=5/);
  await page.getByLabel("关于这段视频，你想了解什么？").fill("再解释一下");
  await page.getByRole("button", { name: "提问 ↗", exact: true }).click();
  await expect(page.locator(".chat-message.assistant")).toHaveCount(2);
  await page.reload();
  await expect(page.locator(".chat-message.assistant")).toHaveCount(2);
  await expect(page.getByText("分析完成", { exact: true })).toBeVisible();
  await page.screenshot({
    path: "test-results/workspace-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/workspace-mobile.png",
    fullPage: true,
  });
  await page.getByRole("link", { name: "← 我的视频", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "我的视频", exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: "fixture video" }),
  ).toBeVisible();
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "删除", exact: true }).click();
  await expect(page).toHaveURL("/");
  await expect(
    page.getByRole("heading", { name: "fixture video" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "继续你的探索" }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "继续你的探索" }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});

test("leaving a workspace cancels a delayed read and stops analysis polling", async ({
  page,
}) => {
  await page.route("https://www.youtube-nocookie.com/**", (r) =>
    r.fulfill({ contentType: "text/html", body: "fixture" }),
  );
  await page.goto("/");
  await page.getByRole("button", { name: "第一次来？创建账号" }).click();
  await page
    .getByLabel("邮箱", { exact: true })
    .fill(`navigation-${Date.now()}@example.com`);
  await page
    .getByLabel("密码", { exact: true })
    .fill("navigation-long-password");
  await page.getByRole("button", { name: "创建账号", exact: true }).click();
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let started!: () => void;
  const waiting = new Promise<void>((resolve) => {
    started = resolve;
  });
  await page.route("**/api/media/*/transcript", async (route) => {
    const response = await route.fetch();
    started();
    await gate;
    await route.fulfill({ response }).catch(() => {});
  });
  await page
    .getByLabel("视频链接", { exact: true })
    .fill("https://youtu.be/dQw4w9WgXcQ");
  await page.getByRole("button", { name: "导入视频" }).click();
  await waiting;
  await page.getByRole("link", { name: "← 视频库", exact: true }).click();
  release();
  await expect(
    page.getByRole("heading", { name: "我的视频", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "fixture video", exact: true }),
  ).toBeVisible();
  await page.unroute("**/api/media/*/transcript");
  await page.clock.install();
  let reads = 0;
  await page.route("**/api/media/*/analysis", async (route) => {
    reads++;
    const response = await route.fetch();
    const body = await response.json();
    body.job = { ...body.job, status: "RUNNING" };
    await route.fulfill({ response, json: body });
  });
  await page.getByRole("link", { name: "fixture video", exact: true }).click();
  await expect.poll(() => reads).toBeGreaterThan(0);
  await page.getByRole("link", { name: "← 我的视频", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "我的视频", exact: true }),
  ).toBeVisible();
  const before = reads;
  await page.clock.fastForward(10000);
  expect(reads).toBe(before);
  await expect(
    page.getByRole("heading", { name: "我的视频", exact: true }),
  ).toBeVisible();
});
