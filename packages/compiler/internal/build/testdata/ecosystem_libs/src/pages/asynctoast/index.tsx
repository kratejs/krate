import SaveButton from '../../components/SaveButton';

export default async function Page() {
  const title = await Promise.resolve('a');
  return <main><h1>{title}</h1><SaveButton /></main>;
}
