import { api } from "./api.js";

export const tables = $state({
  list: [],
  current: localStorage.getItem("currentTable") || null,
});

export async function refreshTables() {
  tables.list = await api("/api/tables");
  if (tables.current && !tables.list.some((t) => t.name === tables.current)) {
    closeTable();
  }
  if (!tables.current && tables.list.length > 0) {
    openTable(tables.list[0].name);
  }
}

export function openTable(name) {
  tables.current = name;
  localStorage.setItem("currentTable", name);
}

export function closeTable() {
  tables.current = null;
  localStorage.removeItem("currentTable");
}
