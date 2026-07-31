/**
 * Upgrade-property scooping: a host (or framework) may assign properties like
 * `controller` or `turns` on an element BEFORE `customElements.define` ran
 * (SSR hosts import `./define` client-side only). Those assignments land as
 * own data properties that shadow the class accessors after upgrade. Called
 * from `connectedCallback`, this deletes each shadowing own property and
 * replays the value through the real setter.
 */
export function upgradeOwnProperties(element: HTMLElement): void {
  for (const name of Object.getOwnPropertyNames(element)) {
    const own = Object.getOwnPropertyDescriptor(element, name);
    if (!own || !("value" in own)) continue;
    let proto: object | null = Object.getPrototypeOf(element);
    let accessor: PropertyDescriptor | undefined;
    while (proto && proto !== HTMLElement.prototype) {
      accessor = Object.getOwnPropertyDescriptor(proto, name);
      if (accessor) break;
      proto = Object.getPrototypeOf(proto);
    }
    if (!accessor?.set) continue;
    const value = own.value as unknown;
    delete (element as unknown as Record<string, unknown>)[name];
    (element as unknown as Record<string, unknown>)[name] = value;
  }
}
