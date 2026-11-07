import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/shadcn-radix/accordion"
import { Checkbox } from "@/components/shadcn-radix/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/shadcn-radix/dialog"
import { Switch } from "@/components/shadcn-radix/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/shadcn-radix/tabs"

// Components as `shadcn add` writes them: each wraps a primitive of the
// radix-ui umbrella package (`import { Dialog as DialogPrimitive }`) in a
// function of the same name. Several of them share one page.
export default function ShadcnRadix() {
  return (
    <main>
      <h1>shadcn-style Radix components</h1>
      <Dialog>
        <DialogTrigger id="sx-dialog">Open dialog</DialogTrigger>
        <DialogContent>
          <DialogTitle>Hello dialog</DialogTitle>
          <DialogDescription>Dialog body</DialogDescription>
        </DialogContent>
      </Dialog>
      <Tabs defaultValue="a">
        <TabsList>
          <TabsTrigger value="a">Tab A</TabsTrigger>
          <TabsTrigger value="b" id="sx-tab-b">Tab B</TabsTrigger>
        </TabsList>
        <TabsContent value="a">Panel A</TabsContent>
        <TabsContent value="b">Panel B</TabsContent>
      </Tabs>
      <Accordion type="single" collapsible>
        <AccordionItem value="x">
          <AccordionTrigger id="sx-accordion">Question</AccordionTrigger>
          <AccordionContent>Answer text</AccordionContent>
        </AccordionItem>
      </Accordion>
      <Checkbox id="sx-checkbox" />
      <Switch id="sx-switch" />
    </main>
  )
}
