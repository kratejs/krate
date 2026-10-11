import { Tabs } from '@krate/components';

export default function TabsDemo() {
  return (
    <div>
      <h1>Tabs (zero-JS)</h1>
      <Tabs defaultValue="overview">
        <Tabs.List>
          <Tabs.Trigger value="overview">Overview</Tabs.Trigger>
          <Tabs.Trigger value="features">Features</Tabs.Trigger>
          <Tabs.Trigger value="pricing">Pricing</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="overview">Overview panel.</Tabs.Content>
        <Tabs.Content value="features">Features panel.</Tabs.Content>
        <Tabs.Content value="pricing">Pricing panel.</Tabs.Content>
      </Tabs>
    </div>
  );
}
