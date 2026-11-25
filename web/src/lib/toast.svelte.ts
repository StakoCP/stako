export type ToastKind = 'success' | 'error' | 'info';

export interface ToastLink {
  href: string;
  label: string;
}

export interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
  link?: ToastLink;
}

let nextId = 1;
let items = $state<ToastItem[]>([]);

export const toast = {
  get items(): ToastItem[] {
    return items;
  },
  show(message: string, kind: ToastKind = 'info', durationMs = 4500, link?: ToastLink) {
    const id = nextId++;
    items = [...items, { id, kind, message, link }];
    if (durationMs > 0 && typeof window !== 'undefined') {
      window.setTimeout(() => {
        this.dismiss(id);
      }, durationMs);
    }
  },
  success(message: string, link?: ToastLink) {
    this.show(message, 'success', 4500, link);
  },
  error(message: string) {
    this.show(message, 'error', 6000);
  },
  info(message: string, link?: ToastLink) {
    this.show(message, 'info', 6000, link);
  },
  dismiss(id: number) {
    items = items.filter((item) => item.id !== id);
  },
};
