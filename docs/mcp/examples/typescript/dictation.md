```ts
const form = new FormData();
form.append("audio", file);
form.append("language", "en");

const response = await fetch(`${process.env.SPEECHKIT_SERVER_URL}/v1/dictation/transcribe`, {
  method: "POST",
  headers: { Authorization: `Bearer ${process.env.SPEECHKIT_TOKEN}` },
  body: form,
});
console.log(await response.json());
```
