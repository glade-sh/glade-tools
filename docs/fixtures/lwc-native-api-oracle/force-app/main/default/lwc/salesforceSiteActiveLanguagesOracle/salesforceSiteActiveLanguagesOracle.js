import { LightningElement } from "lwc";
import value from "@salesforce/site/activeLanguages";

export default class Oracle extends LightningElement {
  label = "@salesforce/site/activeLanguages";
  activeLanguages = value;
}
