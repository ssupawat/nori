function load() {
  return {
    token: localStorage.getItem("token") || "",
    username: localStorage.getItem("username") || "",
    role: localStorage.getItem("role") || "",
  };
}

export const auth = $state(load());

export function setSession({ token, username, role }) {
  auth.token = token;
  auth.username = username;
  auth.role = role;
  localStorage.setItem("token", token);
  localStorage.setItem("username", username);
  localStorage.setItem("role", role);
}

export function clearSession() {
  auth.token = "";
  auth.username = "";
  auth.role = "";
  for (const k of ["token", "username", "role"]) localStorage.removeItem(k);
}

export function canEdit() {
  return auth.role === "admin" || auth.role === "editor";
}
