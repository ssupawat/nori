function load() {
  return {
    token: localStorage.getItem("token") || "",
    username: localStorage.getItem("username") || "",
    role: localStorage.getItem("role") || "",
    // why the last session ended: "expired" shows a notice on the login page
    ended: "",
  };
}

export const auth = $state(load());

export function setSession({ token, username, role }) {
  auth.token = token;
  auth.username = username;
  auth.role = role;
  auth.ended = "";
  localStorage.setItem("token", token);
  localStorage.setItem("username", username);
  localStorage.setItem("role", role);
}

// reason "expired" keeps the username so the login form is prefilled and
// the same user lands back where they were
export function clearSession(reason = "") {
  auth.token = "";
  auth.role = "";
  auth.ended = reason;
  if (reason !== "expired") auth.username = "";
  for (const k of ["token", "username", "role"]) localStorage.removeItem(k);
}

export function canEdit() {
  return auth.role === "admin" || auth.role === "editor";
}
