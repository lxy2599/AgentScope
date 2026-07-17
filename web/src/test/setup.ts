import "@testing-library/jest-dom";

// Polyfill ResizeObserver for jsdom (required by Ant Design)
class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

window.ResizeObserver = ResizeObserverMock as unknown as typeof ResizeObserver;

// Polyfill matchMedia for jsdom (required by Ant Design)
Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

// Safely wrap getComputedStyle to handle pseudoElt (required by Ant Design Table)
const originalGetComputedStyle = window.getComputedStyle;
window.getComputedStyle = (elt: Element, pseudoElt?: string | null) => {
  if (pseudoElt) {
    return { getPropertyValue: () => "" } as unknown as CSSStyleDeclaration;
  }
  return originalGetComputedStyle(elt);
};
