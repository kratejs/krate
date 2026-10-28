import TemplateMark from "./components/TemplateMark";

export default function Template({ children }: { children: React.ReactNode }) {
  return (
    <div id="tpl">
      <TemplateMark />
      {children}
    </div>
  );
}
