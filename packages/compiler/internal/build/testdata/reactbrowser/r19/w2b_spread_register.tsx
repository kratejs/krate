import { useState } from "react";

function useForm() {
  const [values, setValues] = useState<Record<string, string>>({});
  const register = (name: string) => ({
    name,
    value: values[name] ?? "",
    onChange: (e: any) => setValues((v) => ({ ...v, [name]: e.target.value })),
  });
  return { register, values };
}

export default function SpreadRegister() {
  const { register, values } = useForm();
  return (
    <form>
      <input id="email" {...register("email")} />
      <input id="name" {...register("name")} />
      <p id="out">{values.email ?? ""}|{values.name ?? ""}</p>
    </form>
  );
}
