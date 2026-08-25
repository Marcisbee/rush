import { expect, test } from "@rush/browser";

test.session({ clients: ["alice", "bob"] })("isolates realtime application clients", async ({ client }) => {
  const applicationURL = new URL("/__rush/session", location.href).href;
  const alice = client("alice");
  const bob = client("bob");

  await Promise.all([alice.goto(applicationURL), bob.goto(applicationURL)]);
  const [aliceState, bobState] = await Promise.all([
    alice.evaluate(() => {
      localStorage.setItem("identity", "alice");
      sessionStorage.setItem("room", "alpha");
      document.cookie = "rush-client=alice; path=/";
      return { identity: localStorage.getItem("identity"), room: sessionStorage.getItem("room"), cookie: document.cookie };
    }),
    bob.evaluate(() => ({
      identity: localStorage.getItem("identity"),
      room: sessionStorage.getItem("room"),
      cookie: document.cookie,
    })),
  ]);

  expect(aliceState).toEqual({ identity: "alice", room: "alpha", cookie: "rush-client=alice" });
  expect(bobState).toEqual({ identity: null, room: null, cookie: "" });
  expect(alice.url()).toBe(applicationURL);
  expect(bob.url()).toBe(applicationURL);
});

test.session({ clients: ["alice", "bob"] })("resets pooled clients between tests", async ({ clients }) => {
  const applicationURL = new URL("/__rush/session", location.href).href;
  await Promise.all(clients.map((client) => client.goto(applicationURL)));
  const states = await Promise.all(clients.map((client) => client.evaluate(() => ({
    identity: localStorage.getItem("identity"),
    room: sessionStorage.getItem("room"),
    cookie: document.cookie,
  }))));
  expect(states).toEqual([
    { identity: null, room: null, cookie: "" },
    { identity: null, room: null, cookie: "" },
  ]);
});
