import * as api from "./api.js";
const app = document.querySelector("#app"),
  nav = document.querySelector("#nav");
let me = null,
  generation = 0,
  timer = null;
function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "class") node.className = value;
    else if (value !== false && value != null)
      node.setAttribute(key, value === true ? "" : value);
  }
  for (const child of children.flat()) {
    if (child != null)
      node.append(
        child instanceof Node ? child : document.createTextNode(String(child)),
      );
  }
  return node;
}
const link = (text, href, cls = "") => el("a", { href, class: cls }, text);
const money = (n) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: n % 100 ? 2 : 0,
  }).format(n / 100);
const statusName = (s) =>
  ({
    draft: "Черновик",
    published: "Опубликовано",
    withdrawn: "Снято с продажи",
  })[s] || s;
function notice(text) {
  return el("p", { class: "notice", role: "status" }, text);
}
function errorBox() {
  return el("p", { class: "error", role: "alert" });
}
function field(label, name, type = "text", value = "") {
  const input = el(type === "textarea" ? "textarea" : "input", {
    name,
    id: name,
    ...(type === "textarea" ? {} : { type }),
    value: type === "textarea" ? null : value,
  });
  if (type === "textarea") input.value = value;
  return { input, node: el("label", { for: name }, label, input) };
}
function setNav() {
  nav.replaceChildren(link("Каталог", "#"));
  if (me) {
    nav.append(
      link("Мои объявления", "#mine"),
      link("Сообщения", "#messages"),
      link(me.name, "#profile"),
      link("Подать объявление", "#edit/new", "button primary"),
    );
  } else
    nav.append(
      link("Войти", "#login"),
      link("Подать объявление", "#login", "button primary"),
    );
}
async function requireUser() {
  if (!me) {
    location.hash = "#login";
    return false;
  }
  return true;
}
function card(l) {
  const photo = l.photos?.[0]?.url;
  return el(
    "a",
    { class: "card", href: "#listing/" + l.id },
    el(
      "div",
      { class: "picture" },
      photo
        ? el("img", { src: photo, alt: l.title, loading: "lazy" })
        : el("span", { class: "placeholder" }, "Фото скоро появится"),
    ),
    el(
      "div",
      { class: "card-body" },
      el("strong", { class: "price" }, money(l.price_minor)),
      el("h3", {}, l.title || "Без названия"),
      el(
        "p",
        { class: "muted" },
        new Date(l.created_at).toLocaleDateString("ru-RU"),
      ),
    ),
  );
}
async function catalog(gen) {
  const categories = (await api.request("/categories")).items;
  if (gen !== generation) return;
  const title = el(
    "section",
    { class: "hero" },
    el(
      "div",
      {},
      el("p", { class: "eyebrow" }, "НАЙДЁТСЯ ЧТО-ТО ВАШЕ"),
      el("h1", {}, "Хорошие вещи.\nНовые истории."),
      el(
        "p",
        {},
        "Найдите нужное или передайте дальше то, что больше не нужно.",
      ),
    ),
    el(
      "div",
      { class: "hero-art", "aria-hidden": "true" },
      el("span", {}, "↗"),
      el("small", {}, "ближе, чем кажется"),
    ),
  );
  const q = field("Что ищете?", "q", "search"),
    min = field("Цена от, ₽", "min", "number"),
    max = field("Цена до, ₽", "max", "number");
  const select = el(
    "select",
    { id: "category", name: "category" },
    el("option", { value: "" }, "Все категории"),
    categories.map((c) => el("option", { value: c.id }, c.name)),
  );
  const form = el(
    "form",
    { class: "search" },
    q.node,
    el("label", { for: "category" }, "Категория", select),
    min.node,
    max.node,
    el("button", { class: "primary", type: "submit" }, "Найти"),
  );
  const results = el("div", { class: "grid" }),
    err = errorBox(),
    pagination = el("div", { class: "pagination" });
  let page = 1;
  async function load() {
    err.textContent = "";
    const params = new URLSearchParams({
      q: q.input.value,
      category_id: select.value,
      page: String(page),
      limit: "20",
    });
    if (min.input.value)
      params.set(
        "min_price",
        String(Math.round(Number(min.input.value) * 100)),
      );
    if (max.input.value)
      params.set(
        "max_price",
        String(Math.round(Number(max.input.value) * 100)),
      );
    try {
      const data = await api.request("/search?" + params);
      if (gen !== generation) return;
      results.replaceChildren(...data.items.map(card));
      if (!data.items.length)
        results.append(
          notice(
            "Пока ничего не найдено. Попробуйте изменить запрос или фильтры.",
          ),
        );
      pagination.replaceChildren();
      if (page > 1)
        pagination.append(
          el(
            "button",
            {
              onclick: () => {
                page--;
                load();
              },
            },
            "← Назад",
          ),
        );
      pagination.append(el("span", {}, "Страница " + page));
      if (data.has_more)
        pagination.append(
          el(
            "button",
            {
              onclick: () => {
                page++;
                load();
              },
            },
            "Дальше →",
          ),
        );
    } catch (e) {
      err.textContent = e.message;
    }
  }
  form.onsubmit = (e) => {
    e.preventDefault();
    page = 1;
    load();
  };
  app.replaceChildren(
    title,
    form,
    el("h2", {}, "Свежие объявления"),
    err,
    results,
    pagination,
  );
  await load();
}
async function authPage(register) {
  const name = field("Имя", "name"),
    email = field("Email", "email", "email"),
    password = field("Пароль", "password", "password"),
    err = errorBox();
  email.input.required = true;
  password.input.required = true;
  password.input.autocomplete = register ? "new-password" : "current-password";
  password.input.minLength = 12;
  const form = el(
    "form",
    { class: "panel auth" },
    el("p", { class: "eyebrow" }, "ДОБРО ПОЖАЛОВАТЬ"),
    el("h1", {}, register ? "Давайте знакомиться" : "С возвращением"),
    register ? name.node : null,
    email.node,
    password.node,
    register ? el("small", {}, "Пароль от 12 символов.") : null,
    err,
    el(
      "button",
      { class: "primary", type: "submit" },
      register ? "Зарегистрироваться" : "Войти",
    ),
    link(
      register ? "Уже есть аккаунт" : "Создать аккаунт",
      register ? "#login" : "#register",
    ),
  );
  form.onsubmit = async (e) => {
    e.preventDefault();
    err.textContent = "";
    try {
      const body = { email: email.input.value, password: password.input.value };
      if (register) body.name = name.input.value;
      await api.request(register ? "/auth/register" : "/auth/login", {
        method: "POST",
        body,
      });
      me = await api.currentUser();
      setNav();
      location.hash = "#";
    } catch (e) {
      err.textContent = e.message;
    }
  };
  app.replaceChildren(form);
}
async function listing(id, gen) {
  const l = await api.request("/listings/" + id);
  const seller = await api.request("/users/" + l.owner_id);
  if (gen !== generation) return;
  const photos = el(
    "div",
    { class: "gallery" },
    l.photos.map((p) => el("img", { src: p.url, alt: l.title })),
  );
  const err = errorBox();
  const action =
    me?.id === l.owner_id
      ? link("Редактировать", "#edit/" + id, "button primary")
      : el(
          "button",
          {
            class: "primary",
            disabled: l.status !== "published",
            onclick: async () => {
              if (!(await requireUser())) return;
              try {
                const c = await api.request("/conversations", {
                  method: "POST",
                  body: { listing_id: id },
                });
                location.hash = "#chat/" + c.id;
              } catch (e) {
                err.textContent = e.message;
              }
            },
          },
          "Написать продавцу",
        );
  app.replaceChildren(
    link("← В каталог", "#"),
    el(
      "div",
      { class: "detail" },
      el(
        "section",
        {},
        photos,
        el("h2", {}, "Описание"),
        el("p", { class: "description" }, l.description),
      ),
      el(
        "aside",
        { class: "panel" },
        el("span", { class: "badge" }, statusName(l.status)),
        el("h1", {}, l.title),
        el("p", { class: "big-price" }, money(l.price_minor)),
        el("p", {}, "Продавец: " + seller.name),
        action,
        err,
        el(
          "small",
          {},
          "Оплата и передача товара — по договорённости с продавцом.",
        ),
      ),
    ),
  );
}
async function mine(gen) {
  if (!(await requireUser())) return;
  const section = el("div", { class: "list" });
  app.replaceChildren(
    el(
      "div",
      { class: "title-row" },
      el("h1", {}, "Мои объявления"),
      link("Создать объявление", "#edit/new", "button primary"),
    ),
    section,
  );
  let cursor = "";
  const more = el("button", { onclick: load }, "Показать ещё");
  async function load() {
    const d = await api.request("/me/listings?limit=20&cursor=" + cursor);
    if (gen !== generation) return;
    more.remove();
    for (const l of d.items)
      section.append(
        el(
          "article",
          { class: "list-row" },
          el(
            "div",
            {},
            el("span", { class: "badge" }, statusName(l.status)),
            el("h3", {}, l.title || "Без названия"),
            el("p", {}, money(l.price_minor)),
          ),
          link("Редактировать", "#edit/" + l.id, "button"),
        ),
      );
    cursor = d.next_cursor;
    if (cursor) section.append(more);
    if (!section.children.length)
      section.append(
        notice("Здесь появятся ваши объявления. Начните с первого."),
      );
  }
  await load();
}
function priceMinor(value) {
  if (!/^\d+(\.\d{1,2})?$/.test(value || "0"))
    throw new Error("Укажите цену с точностью до копеек");
  const [rub, kop = ""] = (value || "0").split(".");
  const n = Number(rub) * 100 + Number(kop.padEnd(2, "0"));
  if (!Number.isSafeInteger(n)) throw new Error("Слишком большая цена");
  return n;
}
async function editor(id, gen) {
  if (!(await requireUser())) return;
  const categories = (await api.request("/categories")).items;
  let l =
    id === "new"
      ? {
          title: "",
          description: "",
          category_id: "",
          price_minor: 0,
          status: "draft",
          photos: [],
        }
      : await api.request("/listings/" + id);
  if (gen !== generation) return;
  const title = field("Название", "title", "text", l.title),
    description = field("Описание", "description", "textarea", l.description),
    price = field("Цена, ₽", "price", "number", String(l.price_minor / 100));
  price.input.step = "0.01";
  price.input.min = "0";
  title.input.maxLength = 120;
  description.input.maxLength = 10000;
  const category = el(
    "select",
    { id: "category" },
    el("option", { value: "" }, "Выберите категорию"),
    categories.map((c) => el("option", { value: c.id }, c.name)),
  );
  category.value = l.category_id;
  const err = errorBox(),
    info = notice(""),
    badge = el("span", { class: "badge" }, statusName(l.status)),
    photos = el("div", { class: "photo-strip" });
  const saveButton = el(
    "button",
    { type: "submit" },
    l.status === "draft" ? "Сохранить черновик" : "Сохранить изменения",
  );
  async function save() {
    const input = {
      title: title.input.value,
      description: description.input.value,
      category_id: category.value,
      price_minor: priceMinor(price.input.value),
    };
    l = await api.request(l.id ? "/listings/" + l.id : "/listings", {
      method: l.id ? "PATCH" : "POST",
      body: { ...input, ...(l.id ? { expected_version: l.version } : {}) },
    });
    if (id === "new") {
      id = l.id;
      history.replaceState(null, "", "#edit/" + id);
    }
    info.textContent = "Изменения сохранены";
    return l;
  }
  async function action(fn) {
    err.textContent = "";
    try {
      await fn();
    } catch (e) {
      err.textContent = e.message;
    }
  }
  function drawPhotos() {
    photos.replaceChildren(
      ...l.photos.map((p) =>
        el(
          "div",
          {},
          el("img", { src: p.url, alt: "Фото объявления" }),
          el(
            "button",
            {
              type: "button",
              onclick: () =>
                action(async () => {
                  await api.request("/listings/" + l.id + "/photos/" + p.id, {
                    method: "DELETE",
                  });
                  l = await api.request("/listings/" + l.id);
                  drawPhotos();
                }),
            },
            "Удалить",
          ),
        ),
      ),
    );
  }
  const file = el("input", {
    type: "file",
    id: "photo",
    accept: "image/jpeg,image/png",
  });
  file.onchange = () =>
    action(async () => {
      if (!file.files.length) return;
      await save();
      const data = new FormData();
      data.append("file", file.files[0]);
      await api.request("/listings/" + l.id + "/photos", {
        method: "POST",
        body: data,
      });
      l = await api.request("/listings/" + l.id);
      drawPhotos();
      file.value = "";
      info.textContent = "Фотография добавлена";
    });
  const publish = el(
    "button",
    {
      class: "primary",
      type: "button",
      onclick: () =>
        action(async () => {
          await save();
          l = await api.request("/listings/" + l.id + "/publish", {
            method: "POST",
            body: { expected_version: l.version },
          });
          badge.textContent = statusName(l.status);
          publish.hidden = true;
          withdraw.hidden = false;
          saveButton.textContent = "Сохранить изменения";
          info.textContent =
            "Объявление опубликовано. В поиске оно появится через несколько секунд.";
        }),
    },
    "Опубликовать",
  );
  const withdraw = el(
    "button",
    {
      type: "button",
      onclick: () =>
        action(async () => {
          l = await api.request("/listings/" + l.id + "/withdraw", {
            method: "POST",
            body: { expected_version: l.version },
          });
          badge.textContent = statusName(l.status);
          publish.hidden = false;
          withdraw.hidden = true;
          info.textContent = "Объявление снято с продажи";
        }),
    },
    "Снять с продажи",
  );
  publish.hidden = l.status === "published";
  withdraw.hidden = l.status !== "published";
  const form = el(
    "form",
    { class: "panel editor" },
    el(
      "div",
      { class: "title-row" },
      el(
        "h1",
        {},
        id === "new" ? "Новое объявление" : "Редактировать объявление",
      ),
      badge,
    ),
    title.node,
    description.node,
    el(
      "div",
      { class: "two-columns" },
      el("label", { for: "category" }, "Категория", category),
      price.node,
    ),
    el("h2", {}, "Фотографии"),
    el("small", {}, "До 8 фотографий, JPEG или PNG, до 5 МБ каждая."),
    photos,
    el("label", { for: "photo" }, "Добавить фотографию", file),
    info,
    err,
    el("div", { class: "actions" }, saveButton, publish, withdraw),
  );
  form.onsubmit = (e) => {
    e.preventDefault();
    action(save);
  };
  drawPhotos();
  app.replaceChildren(form);
}
async function messages(gen) {
  if (!(await requireUser())) return;
  const list = el("div", { class: "list" }),
    err = errorBox();
  let cursor = "",
    busy = false;
  const more = el("button", { onclick: load }, "Показать ещё");
  app.replaceChildren(el("h1", {}, "Сообщения"), err, list);
  async function load() {
    if (busy) return;
    busy = true;
    try {
      const d = await api.request(
        "/conversations?limit=20&cursor=" + encodeURIComponent(cursor),
      );
      if (gen !== generation) return;
      more.remove();
      for (const c of d.items) {
        const row = link("", "#chat/" + c.id, "conversation");
        row.append(
          el("strong", {}, c.peer_name),
          el("span", {}, c.title),
          el("small", {}, c.last_message || "Начните разговор"),
        );
        list.append(row);
      }
      cursor = d.next_cursor;
      if (cursor) list.append(more);
      if (!list.children.length)
        list.append(
          notice(
            "Пока нет переписок. Напишите продавцу из карточки объявления.",
          ),
        );
    } catch (e) {
      err.textContent = e.message;
    } finally {
      busy = false;
    }
  }
  await load();
}
async function chat(id, gen) {
  if (!(await requireUser())) return;
  const feed = el("div", { class: "chat-feed" }),
    body = field("Сообщение", "message", "textarea"),
    err = errorBox();
  let cursor = "0",
    sendingKey = null,
    sending = false,
    draftRevision = 0,
    loading = null;
  const title = el("h1", {}, "Переписка");
  const send = el("button", { class: "primary", type: "submit" }, "Отправить");
  const form = el("form", { class: "chat-compose" }, body.node, send, err);
  app.replaceChildren(link("← Все сообщения", "#messages"), title, feed, form);
  function load() {
    if (!loading)
      loading = loadPages().finally(() => {
        loading = null;
      });
    return loading;
  }
  async function loadPages() {
    try {
      const d = await api.request(
        "/conversations/" + id + "/messages?limit=100&cursor=" + cursor,
      );
      if (gen !== generation) return;
      title.textContent = d.conversation.title;
      for (const m of d.items)
        feed.append(
          el(
            "article",
            { class: "bubble " + (m.sender_id === me.id ? "mine" : "") },
            el("p", {}, m.body),
            el(
              "small",
              {},
              new Date(m.created_at).toLocaleTimeString("ru-RU", {
                hour: "2-digit",
                minute: "2-digit",
              }),
            ),
          ),
        );
      cursor = d.next_cursor;
      if (d.items.length) feed.scrollTop = feed.scrollHeight;
      if (d.items.length === 100) await loadPages();
    } catch (e) {
      err.textContent = e.message;
    }
  }
  form.onsubmit = async (e) => {
    e.preventDefault();
    if (sending || !body.input.value.trim()) return;
    err.textContent = "";
    sendingKey ??= crypto.randomUUID();
    const sentText = body.input.value,
      sentKey = sendingKey,
      sentRevision = draftRevision;
    sending = true;
    send.disabled = true;
    try {
      await api.request("/conversations/" + id + "/messages", {
        method: "POST",
        body: { body: sentText },
        headers: { "Idempotency-Key": sentKey },
      });
      if (draftRevision === sentRevision) body.input.value = "";
      if (sendingKey === sentKey) sendingKey = null;
      await load();
    } catch (e) {
      err.textContent = e.message;
    } finally {
      sending = false;
      send.disabled = false;
    }
  };
  body.input.oninput = () => {
    draftRevision++;
    sendingKey = null;
  };
  await load();
  if (gen === generation) timer = setInterval(load, 5000);
}
async function profile() {
  if (!(await requireUser())) return;
  const name = field("Имя", "name", "text", me.name),
    err = errorBox(),
    info = notice("");
  const form = el(
    "form",
    { class: "panel auth" },
    el("h1", {}, "Мой профиль"),
    name.node,
    el("p", { class: "muted" }, me.email),
    el("small", {}, "Email виден только вам."),
    err,
    info,
    el("button", { class: "primary", type: "submit" }, "Сохранить"),
    el(
      "button",
      {
        type: "button",
        onclick: async () => {
          try {
            await api.logout();
            me = null;
            setNav();
            location.hash = "#";
          } catch (e) {
            err.textContent = e.message;
          }
        },
      },
      "Выйти",
    ),
  );
  form.onsubmit = async (e) => {
    e.preventDefault();
    try {
      me = await api.request("/me", {
        method: "PATCH",
        body: { name: name.input.value },
      });
      setNav();
      info.textContent = "Профиль сохранён";
    } catch (e) {
      err.textContent = e.message;
    }
  };
  app.replaceChildren(form);
}
async function route() {
  generation++;
  const gen = generation;
  clearInterval(timer);
  timer = null;
  const [page, id] = location.hash.slice(1).split("/");
  app.replaceChildren(notice("Загрузка…"));
  try {
    if (page === "login" || page === "register")
      await authPage(page === "register");
    else if (page === "listing") await listing(id, gen);
    else if (page === "edit") await editor(id, gen);
    else if (page === "mine") await mine(gen);
    else if (page === "messages") await messages(gen);
    else if (page === "chat") await chat(id, gen);
    else if (page === "profile") await profile();
    else await catalog(gen);
  } catch (e) {
    if (gen === generation)
      app.replaceChildren(
        el("h1", {}, "Не удалось открыть страницу"),
        notice(e.message),
        link("В каталог", "#"),
      );
  }
}
window.addEventListener("hashchange", route);
me = (await api.refresh()) ? await api.currentUser() : null;
setNav();
await route();
